// Package policy provides MantleKeep's DEFAULT, dependency-free policy engine plus
// the failsafe wrapper. It is pure Go — no OPA, no heavy transitive tree — so the
// core binary links almost nothing. OPA remains available as an OPT-IN engine in a
// SEPARATE module (mantlekeep.dev/opa), injected via app.Options.Policy — so OPA's
// dependency tree never enters the core module's graph (nor a security scan of it).
//
// RBAC mirrors authz.rego exactly (the two must stay in sync): role→action map,
// intent-required, and the AI-cannot-approve guardrail.
package policy

import (
	"context"
	"sort"

	mantlekeep "mantlekeep.dev/control"
)

// roleActions is the role→allowed-actions map. Only the generic L0-SuperAdmin wildcard lives in
// the engine ("*" means all); every product/action grant is DATA, loaded from the shared grants
// document (mantlekeep.dev/control/grants) so the engine names no product. loadRoleActions merges the
// wildcard with that document — see grants_load.go. The grants are EXPLICIT per-role sets, not a
// seniority ladder: a role gets exactly the actions listed for it.
//
// Every action — including env-gated ones like a product's "promote" — is a normal grant here plus,
// where an environment must escalate the required role, a generic `required_role_when` floor RULE in
// the product's floor DATA (grants/floors.json). The engine names NO product verb and NO environment:
// what "promote" means, which environments exist, and who may reach each is all DATA a product owns.
// roleActions() (the lazy, merged accessor) is defined in grants_load.go.

// KnownActions returns every action any role may hold, sorted.
//
// It exists so a parity test can DERIVE what to compare instead of hand-listing it. A fixed
// list only ever checks the actions somebody remembered to add, so a new action can differ
// between this policy and authz.rego while the test still passes — which is exactly what
// happened to workspace.grant and the worker actions.
func KnownActions() []string {
	seen := map[string]bool{}
	for _, actions := range roleActions() {
		for action := range actions {
			if action != "*" {
				seen[action] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for action := range seen {
		out = append(out, action)
	}
	sort.Strings(out)
	return out
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

// approvalActions are the actions an AI may NEVER perform, whatever role it is granted.
// Approval is the human gate: an AI can propose and draft, but a human decides. The action
// NAMES are loaded from the shared grants document (the same source authz.rego reads), so the
// guardrail logic stays generic in the engine and cannot drift from the Rego side.
// approvalActions() (the lazy, merged accessor) is defined in grants_load.go.

// roleRank orders authority (lower = more). A subject holding a role at least as
// senior as an action's required role is permitted it.
var roleRank = map[string]int{
	"L0-SuperAdmin": 0, "L1-Architect": 1, "L2-Operator": 2, "L3-Consumer": 3, "AI-Agent": 4,
}

// ActionAuthorizer lets config-authored products carry their OWN authorization
// binding. A product declared on the Canvas has an action the static map above
// never heard of; its manifest declares RunAs, and that is the minimum role the
// policy requires to run it. This keeps policy honest for runtime-added products
// with no code change — "products are config" extended to the law itself.
type ActionAuthorizer interface {
	RequiredRole(action string) (mantlekeep.Role, bool)
}

// RBAC is the built-in pure-Go PolicyEvaluator.
type RBAC struct {
	dyn ActionAuthorizer // optional: authz for config-authored product actions
	// scopes, when set, resolves policy PER SCOPE at request time: the intent's Scope (a
	// generic tenancy key — SDLC's project, or a tenant/app) selects that scope's cascade
	// (base → team → scope layer, seal enforced). It wins over dyn. See scope.go.
	scopes *ScopeResolver
	// providers are the registered product policy adapters (see provider.go). The core holds
	// NO product action names of its own; each provider supplies its actions, role grants, and
	// an attribute-floor Admit. byAction routes an owned action's admission to its provider;
	// providerRoleActions folds every provider's role grants into the generic role check.
	providers           []PolicyProvider
	byAction            map[string]PolicyProvider
	providerRoleActions map[string]map[string]bool
}

// liveSnapshot is the read side of a LiveResolver: the action authorizer read from an atomic
// snapshot that a watcher can swap. RBAC depends only on this small interface, so the hot-reload
// machinery stays in dynamic.go.
type liveSnapshot interface {
	RequiredRole(action string) (mantlekeep.Role, bool)
}

// NewRBAC returns the default policy engine.
func NewRBAC() *RBAC { return &RBAC{} }

// WithDynamic attaches a runtime authorizer consulted when the static map does
// not cover an action (a Canvas-authored product). Returns the receiver to chain.
func (r *RBAC) WithDynamic(a ActionAuthorizer) *RBAC { r.dyn = a; return r }

// WithProviders registers one or more product policy adapters (see provider.go). Each
// provider's actions become known to the engine, its role grants fold into the generic role
// check, and its Admit is consulted for the attribute floor on the actions it owns. Nil
// providers are skipped. Returns the receiver to chain. This is the compile-time adapter
// wiring — the deployable's main calls it with the adapters it ships; the core package itself
// imports no provider.
func (r *RBAC) WithProviders(ps ...PolicyProvider) *RBAC {
	for _, p := range ps {
		if p == nil {
			continue
		}
		r.providers = append(r.providers, p)
		if r.byAction == nil {
			r.byAction = map[string]PolicyProvider{}
		}
		for _, a := range p.Actions() {
			r.byAction[a] = p
		}
		if r.providerRoleActions == nil {
			r.providerRoleActions = map[string]map[string]bool{}
		}
		for role, acts := range p.RoleActions() {
			m := r.providerRoleActions[string(role)]
			if m == nil {
				m = map[string]bool{}
				r.providerRoleActions[string(role)] = m
			}
			for _, a := range acts {
				m[a] = true
			}
		}
	}
	return r
}

// WithScopes wires per-scope resolution: each decision reads the intent's Scope (a generic
// tenancy key — SDLC maps its project here) and authorizes against that scope's cascade
// (base → team → the scope's own layer), with the sealed floor enforced. It takes precedence
// over WithResolved/WithLive for the resolved action roles. Returns the receiver to chain.
func (r *RBAC) WithScopes(sr *ScopeResolver) *RBAC { r.scopes = sr; return r }

// WithResolved feeds the engine a LAYERED, cascaded config (see Resolve): the resolved
// action authorizer (team over product), with sealed keys already enforced. This is how
// the V3 config cascade (default → product → team) reaches the law. Returns the receiver
// to chain.
func (r *RBAC) WithResolved(res *Resolved) *RBAC {
	r.dyn = res
	return r
}

// WithLive wires the engine to a LiveResolver (see dynamic.go) so the action-role authorizer
// is read from the ATOMIC snapshot on every evaluation. A watcher swaps that snapshot on a
// config change and the very next decision uses the new policy — hot-reload, no restart. The
// sealed floor is already baked into each swapped snapshot by Resolve(), so live config can
// never loosen it. Returns the receiver to chain.
func (r *RBAC) WithLive(l liveSnapshot) *RBAC {
	r.dyn = l
	return r
}

// Evaluate implements mantlekeep.PolicyEvaluator over the TYPED input — no map-digging.
func (r *RBAC) Evaluate(_ context.Context, input mantlekeep.PolicyInput) (mantlekeep.Decision, error) {
	roles := rolesToStrings(input.Subject.Roles)
	subjectID := input.Subject.ID
	isAI := input.Subject.IsAI
	action := input.Intent.Action
	goal := input.Intent.Goal
	requester := input.Intent.Requester

	if goal == "" {
		return deny("intent_spec.goal is required"), nil
	}
	if isAI && approvalActions()[action] {
		return deny("AI agents cannot approve: " + action), nil
	}
	// Pick the effective authorizer for THIS request — per-scope when a ScopeResolver is wired
	// and the intent names a known scope, else the global resolution.
	dyn := r.authorizerFor(input)
	// Every action is a normal grant check. There is no special-cased product verb: an env-gated
	// action (a product's "promote") is a grant PLUS a generic required_role_when floor RULE in the
	// product's floor DATA (applied below), so the engine names no product verb and no environment.
	if !r.actionAllowed(roles, action, dyn) {
		return deny("no role permits action " + action), nil
	}
	// Separation of duties: when a requester is supplied (a run approval), the
	// acting subject must not be that same person — no one approves their own request.
	if requester != "" && requester == subjectID {
		return deny("separation of duties: the approver cannot be the requester"), nil
	}
	// Product admission floor: the role check above decided WHO may issue the action; if a
	// registered provider OWNS this action, its Admit now decides whether THIS concrete request
	// is admissible (an ABAC check over intent.Params). This is fully generic — the core knows
	// no product's floor, only the port. Each provider's Rego bundle mirrors its own Admit; the
	// per-provider drift test keeps them in lockstep.
	if p := r.byAction[action]; p != nil {
		if denied, reason := p.Admit(input.Intent, input.Subject.Roles); denied {
			return deny(reason), nil
		}
	}
	// Generic attribute floor: a product's IT-owned floor is DATA (grants/floors.json), applied
	// by the generic evaluator (floor.go). An action with no floor rules passes untouched, so the
	// engine names no product concept — the param names + values live in the data. The OPA adapter
	// mirrors this over the same data.floors, kept in lockstep by the parity test.
	if denied, reason := admitFloor(action, input.Intent.Params, input.Subject.Roles); denied {
		return deny(reason), nil
	}
	return mantlekeep.Decision{Action: mantlekeep.ActionAllow, PolicyID: "mantlekeep.rbac"}, nil
}

// StepAuth is the per-step authorization request: WHO is triggering WHICH step of a
// running product, the step's optional role floor, and its author (for SoD). It is the
// canvas per-step permission model — a node can require a higher role than the flow, and
// can demand that a DIFFERENT person triggers it than the one who authored it.
type StepAuth struct {
	Subject         mantlekeep.PolicySubject
	Action          string      // the product action (audit context)
	Step            string      // the step name
	RunAs           mantlekeep.Role // per-step minimum role; "" = inherit the product's floor
	Author          string      // who authored/created this step
	RequireApproval bool        // if true, the triggering subject must differ from Author
}

// EvaluateStep authorizes triggering ONE step. It reuses the EXACT rules the door uses
// for actions — holdsAtLeast for the role floor and the approver-≠-requester clause for
// separation of duties — so a step is governed by the same law as everything else, not a
// second engine. AI agents can never trigger an approval-gated step (they cannot break
// SoD by acting as the second person). This mirrors the step block in authz.rego.
func (r *RBAC) EvaluateStep(in StepAuth) mantlekeep.Decision {
	roles := rolesToStrings(in.Subject.Roles)
	// Per-step role floor (a node may demand more authority than the flow).
	if in.RunAs != "" && !holdsAtLeast(roles, in.RunAs) {
		return deny("step " + in.Step + ": no role permits it (requires " + string(in.RunAs) + ")")
	}
	// Per-step separation of duties: the author cannot also trigger it.
	if in.RequireApproval {
		if in.Subject.IsAI {
			return deny("step " + in.Step + ": AI agents cannot satisfy separation of duties")
		}
		if in.Author != "" && in.Author == in.Subject.ID {
			return deny("separation of duties: the author of step " + in.Step + " cannot trigger it")
		}
	}
	return mantlekeep.Decision{Action: mantlekeep.ActionAllow, PolicyID: "mantlekeep.rbac"}
}

// authorizerFor picks the effective action-authorizer for THIS request. When a ScopeResolver is
// wired, the intent's Scope selects that scope's cascade (base → team → scope layer, seal
// enforced) — per-scope policy at request time. Otherwise it returns the global (possibly live) dyn.
func (r *RBAC) authorizerFor(in mantlekeep.PolicyInput) ActionAuthorizer {
	// Only a KNOWN scope is resolved per-scope; empty/unknown scopes fall through to the
	// live/global path below, so wiring scopes never disturbs the base hot-reload.
	if r.scopes != nil && in.Intent.Scope != "" && r.scopes.Has(in.Intent.Scope) {
		return r.scopes.For(in.Intent.Scope)
	}
	return r.dyn
}

func (r *RBAC) actionAllowed(roles []string, action string, dyn ActionAuthorizer) bool {
	for _, ro := range roles {
		acts := roleActions()[ro]
		if acts["*"] || acts[action] {
			return true
		}
		// A registered product provider may grant this role the action — the core keeps no
		// product action names of its own (see WithProviders/provider.go).
		if r.providerRoleActions[ro][action] {
			return true
		}
	}
	// Fall back to the resolved/config-authored binding (team, project, or product RunAs).
	if dyn != nil {
		if need, ok := dyn.RequiredRole(action); ok {
			return holdsAtLeast(roles, need)
		}
	}
	return false
}

// holdsAtLeast reports whether any of the subject's roles is at least as senior
// as need.
func holdsAtLeast(roles []string, need mantlekeep.Role) bool {
	nr, ok := roleRank[string(need)]
	if !ok {
		return false
	}
	for _, r := range roles {
		if rk, ok := roleRank[r]; ok && rk <= nr {
			return true
		}
	}
	return false
}

func deny(reason string) mantlekeep.Decision {
	return mantlekeep.Decision{Action: mantlekeep.ActionDeny, Reason: reason, PolicyID: "mantlekeep.rbac", Warnings: []string{reason}}
}

func rolesToStrings(rs []mantlekeep.Role) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}
