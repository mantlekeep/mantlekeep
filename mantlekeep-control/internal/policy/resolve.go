package policy

import (
	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// This file is the LAYERED CONFIG RESOLVER — MantleKeep's config cascade, the V3 model:
//
//	MantleKeep default → product → department/team → SCOPE override   (most specific wins)
//
// The SCOPE tier is a generic per-request tenancy layer (SDLC maps its "project" onto it;
// other products use a tenant/workspace/app, or none) — see scope.go.
//
// Most keys cascade freely: the most-specific layer that sets a key wins, exactly
// like Spring profiles or Helm values. But a SEALED key is a governance floor — once
// a layer seals it, a lower (more specific) layer may only TIGHTEN it (require a more
// senior role), never loosen, remove, or un-seal it. That is what stops a team from
// "hacking" a mandatory host/regulatory rule by overriding it looser.

// Layer is one tier's contribution to the effective policy config, applied in
// precedence order (least specific first): DefaultLayer(), then product, then team.
type Layer struct {
	Name        string                     // audit/debug label: "mantlekeep", "product:example", "team:example"
	ActionRoles map[string]mantlekeep.Role // action → minimum role permitted to run it
	// Sealed lists keys THIS layer locks as a floor. A later layer may only tighten
	// them. Key form: "action:<name>", e.g. "action:service.deploy".
	Sealed []string
	// Roles, when non-empty, declares the deployment's role vocabulary (name→authority rank,
	// lower = more senior). The FIRST layer that sets it defines the ladder (see LadderFrom);
	// lower layers reference role names, they do not redefine the vocabulary. Empty on almost
	// every layer — a deployment names its tiers once, in the platform/base layer, or not at all.
	Roles map[string]int
}

// Resolved is the effective, cascaded policy config the engine reads. It implements
// ActionAuthorizer over the resolved action→role bindings.
type Resolved struct {
	actionRoles map[string]mantlekeep.Role
	sealed      map[string]bool
	fallback    ActionAuthorizer // consulted after the resolved action roles (e.g. the product registry)
	ladder      RoleLadder       // the deployment's role vocabulary, used for the seal-tightening seniority check
}

// Resolve cascades layers in order. Most-specific wins per key, EXCEPT a sealed key:
// once sealed by a layer, a later layer's override is accepted only if it is at least
// as senior (stricter); a looser or absent override keeps the sealed value, and the
// seal itself can never be lifted by a lower layer.
//
// ladder is the deployment's role vocabulary the seal-tightening check ranks against; a
// nil/empty ladder falls back to the built-in default, so a deployment that names no roles
// resolves exactly as before.
func Resolve(ladder RoleLadder, layers ...Layer) *Resolved {
	if len(ladder) == 0 {
		ladder = DefaultRoleLadder()
	}
	r := &Resolved{
		actionRoles: map[string]mantlekeep.Role{},
		sealed:      map[string]bool{},
		ladder:      ladder,
	}
	for _, l := range layers {
		for action, need := range l.ActionRoles {
			r.apply(r.actionRoles, action, need, "action:"+action)
		}
		// Record this layer's seals AFTER applying its own values, so the layer that
		// establishes the floor sets it freely; only LOWER layers are constrained.
		for _, k := range l.Sealed {
			r.sealed[k] = true
		}
	}
	return r
}

// apply sets an override, enforcing the seal rule: if the key is already sealed by an
// upper layer, refuse any value that is not at least as senior as the current one.
func (r *Resolved) apply(m map[string]mantlekeep.Role, key string, need mantlekeep.Role, sealKey string) {
	if r.sealed[sealKey] {
		if cur, ok := m[key]; ok && !r.ladder.atLeastAsSenior(need, cur) {
			return // a team cannot loosen a sealed floor — override rejected
		}
	}
	m[key] = need
}

// WithFallback chains an authorizer consulted when the resolved layers do not define
// an action's role — the product registry (the product layer of the cascade). Returns
// the receiver to chain.
func (r *Resolved) WithFallback(a ActionAuthorizer) *Resolved { r.fallback = a; return r }

// RequiredRole implements ActionAuthorizer. Resolved (default+team) action roles win;
// then the product registry fallback. The RBAC static baseline is checked before this
// by the engine, so precedence overall is: baseline seniors → team → product.
func (r *Resolved) RequiredRole(action string) (mantlekeep.Role, bool) {
	if need, ok := r.actionRoles[action]; ok {
		return need, true
	}
	if r.fallback != nil {
		return r.fallback.RequiredRole(action)
	}
	return "", false
}

// DefaultLayer is MantleKeep's built-in baseline. It carries NO built-in gates: the engine names no
// action or environment, so there is no default action→role binding to seed here — a team/product
// supplies theirs in their own layer, and env-gating is a product's floor DATA. It remains the
// unsealed base anchor of the cascade (Resolve(DefaultLayer(), teamLayer, ...)).
func DefaultLayer() Layer {
	return Layer{Name: "mantlekeep"}
}

// ScopeLayer is the SCOPE tier of the cascade — the most specific tier, below the team. A
// scope is a generic tenancy key (SDLC calls it a project; other products a tenant/app). It
// self-serves its own action→role bindings WITHOUT touching the team or the host's sealed floor.
// Most-specific-wins applies to free keys; the seal rule still holds — a scope may TIGHTEN a
// sealed floor but can never loosen it. Pass it LAST to Resolve: Resolve(DefaultLayer(),
// teamLayer, ScopeLayer(...)).
func ScopeLayer(scope string, actionRoles map[string]mantlekeep.Role, sealed []string) Layer {
	return Layer{Name: "scope:" + scope, ActionRoles: actionRoles, Sealed: sealed}
}
