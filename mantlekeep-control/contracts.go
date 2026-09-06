// Package mantlekeep defines the core contracts of the MantleKeep control plane.
//
// This file is the spine. By design: interfaces
// before implementations. Everything — humans and AI agents — passes through
// SDK.Submit(intent). The SDK is the only door; there is no bypass.
//
// Design rules honoured here:
//   - Near-zero deps: this file imports stdlib only (context, time).
//   - Small interfaces (1–3 methods): easy for humans AND AI to grasp.
//   - Every interface has an embedded impl; a wire (HTTP) impl composes across process boundaries.
//   - Vendor-agnostic: the policy port hides the backend — the default is a built-in generic engine
//     (no external deps); OPA/Cedar are opt-in adapters (OPA compiled to WASM, loaded in-process).
package mantlekeep

import (
	"context"
	"time"
)

// ContractVersion is the semantic version of the PORTS in this file — the hexagon
// boundary every adapter (in-tree or a downstream host plugin) implements. Bump the
// MAJOR on any breaking change to an interface signature or a wire shape (the policy
// input, the manifest schema, the wasm ABI); bump MINOR for additive, back-compatible
// changes. Downstream pins this. The arch guard test enforces that the core stays
// dependency-free so these ports never drag an adapter's concerns into the domain.
const ContractVersion = "3.0.0"

// ─────────────────────────────────────────────────────────────────────────────
// Identity — WHO is acting (RBAC). AD group is the single source of truth.
// ─────────────────────────────────────────────────────────────────────────────

// Role is the coarse authority tier. Ordered L0 (most) → AI-Agent (least).
type Role string

const (
	RoleSuperAdmin Role = "L0-SuperAdmin" // full authority
	RoleArchitect  Role = "L1-Architect"  // elevated operations
	RoleOperator   Role = "L2-Operator"   // standard operations
	RoleConsumer   Role = "L3-Consumer"   // read + trigger
	RoleAIAgent    Role = "AI-Agent"      // service accounts — propose only, never approve
)

// Subject is a resolved actor: a human (via AD group) or an AI service account.
type Subject struct {
	ID       string            // stable identifier (AD user, or service-account id)
	Roles    []Role            // effective roles, derived from AD group membership
	ADGroups []string          // source-of-truth group memberships
	IsAI     bool              // true for AI agents — triggers extra guardrails
	Attrs    map[string]string // verified ABAC attributes (department, clearance…), optional
}

// ExternalIdentity is the VERIFIED external identity handed to the resolver — the
// stable id plus, for claims-based backends (an SSO gateway), the IdP groups and
// extra attributes the gateway asserted. The resolver maps these to a Subject
// SERVER-SIDE; a caller never asserts roles directly (Subject.Roles is not trusted
// as an input to the door). This is what keeps "the door decides what you may be".
type ExternalIdentity struct {
	ID     string
	Groups []string          // verified IdP groups (empty for the dev tier)
	Attrs  map[string]string // extra verified claims (department, clearance…) for ABAC
}

// IdentityResolver maps a verified external identity to a Subject with effective
// roles. Impl: mock (MVP, by id) → gateway (groups→roles) → AD/LDAP (production).
type IdentityResolver interface {
	Resolve(ctx context.Context, ext ExternalIdentity) (Subject, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Intent — WHAT is being requested. Mandatory on every SDK.Submit.
// No intent spec → SDK rejects. This forces declare-before-execute.
// ─────────────────────────────────────────────────────────────────────────────

// RiskLevel is the self-assessed risk; OPA validates/overrides it.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// IntentSpec is the mandatory declaration of what an action should do and why.
// It is the DECLARED intent. (Verifying that behaviour MATCHES the declaration is NOT built —
// the chain proves a governed step occurred, in order, by whom; it does not prove the step did
// what its goal describes. Do not claim otherwise in a demo.)
type IntentSpec struct {
	Goal            string    // "Deploy example-service v2.3.1 to production"
	AffectedSystems []string  // blast radius — systems/files/services touched
	RiskLevel       RiskLevel // self-assessed, validated by policy
	Preconditions   []string  // must be true before ("all tests pass")
	Postconditions  []string  // expected state after ("/health returns 200")
	RollbackPlan    string    // how to undo
}

// Intent is a single governed request submitted through the one door.
type Intent struct {
	ID          string     // unique, e.g. INT-2026-07-001
	Subject     Subject    // who (resolved identity)
	Action      string     // the SDK action name, e.g. "job.run"
	Resource    string     // scope target, e.g. "project/demo"
	Spec        IntentSpec // mandatory — see above
	Params      map[string]any
	SubmittedAt time.Time
	TTL         time.Duration // expiry; expired intents are rejected

	// Via names the application that ASSERTED this subject, when the subject was not
	// the authenticated caller. The door authenticates an application; that application
	// says who it acts for. Both belong on the record: without Subject the chain cannot
	// say who acted, and without Via it cannot say which service carried the claim.
	// Empty when the caller acted as itself.
	Via string
}

// ─────────────────────────────────────────────────────────────────────────────
// Policy — the LAW. A built-in generic floor engine (dependency-free) decides ALLOW / DENY /
// REQUIRE_APPROVAL over EXTERNAL policy data; OPA/Cedar are opt-in adapters, never embedded here.
// ─────────────────────────────────────────────────────────────────────────────

// DecisionAction is the outcome of policy evaluation.
type DecisionAction string

const (
	ActionAllow           DecisionAction = "allow"
	ActionDeny            DecisionAction = "deny"
	ActionRequireApproval DecisionAction = "require_approval"
)

// DenialCategory is the GENERIC reason a decision was not an allow — the same register
// of core vocabulary as DecisionAction, Role and RiskLevel: it names governance shapes,
// never a product, action, environment or role. It exists so a consumer can branch on a
// STABLE value the engine stamps at the point of denial, instead of parsing the free-text
// Reason (which a reword would silently reclassify). A wire adapter maps each category to
// its transport code; the core owns the category, the boundary owns the code.
//
// Empty (DenialNone) on an allow, or on a Decision from an external evaluator that does not
// set it — a wire adapter then falls back to its own classification for those.
type DenialCategory string

const (
	DenialNone               DenialCategory = ""                     // an allow, or unclassified
	DenialFloor              DenialCategory = "floor"                // a policy floor blocked it (a cap, a pin, an admission rule, failsafe read-only)
	DenialSeparationOfDuties DenialCategory = "separation_of_duties" // the actor may not also be the approver — includes an AI attempting to approve
	DenialIdentity           DenialCategory = "identity"             // the caller could not be resolved, or may not act for whom it claimed
	DenialActionNotAllowed   DenialCategory = "action_not_allowed"   // no role or grant permits this action
	DenialValidation         DenialCategory = "validation"           // the request itself is malformed or incomplete
	DenialPolicyError        DenialCategory = "policy_error"         // the engine could not reach a verdict
)

// Decision is the policy verdict on an intent.
type Decision struct {
	Action DecisionAction
	Reason string // human-readable justification
	// Category is the generic, stable classification of a non-allow (see DenialCategory).
	// The engine sets it at the point of denial so a consumer never has to parse Reason.
	// omitempty keeps an allow (and any pre-existing marshalled record) byte-identical.
	Category          DenialCategory `json:"category,omitempty"`
	RequiredApprovers []Role         // for require_approval — who may sign off
	PolicyID          string         // which policy produced this (audit)
	Warnings          []string       // non-blocking flags (e.g. budget at 85%)
}

// DecisionError carries the full Decision out of Submit when the outcome is not an allow.
// Submit returns (token, error); without this, a deny or require_approval would reach the
// caller as a bare string and lose the fields an enterprise wire must surface — which
// policy decided, who may approve, the structured reason. A caller type-asserts to recover
// the Decision; a plain error still reads sensibly for callers that only log it.
type DecisionError struct {
	Decision Decision
}

func (e *DecisionError) Error() string {
	return string(e.Decision.Action) + ": " + e.Decision.Reason
}

// Refused is a typed refusal: what the door decided, why, and who can unblock it.
//
// An error string alone loses the distinction that matters most. "deny" is final; a
// require_approval is a change WAITING for a person, and a caller that cannot tell them
// apart reports a pending approval as a failure — which is how a governed change looks
// broken to the person who submitted it.
type Refused struct {
	Action DecisionAction
	Reason string
	// RequiredApprovers names who may sign off, when the decision was require_approval. A
	// refusal that cannot say who unblocks it is a dead end wearing the shape of a process.
	RequiredApprovers []Role
	PolicyID          string
}

func (r *Refused) Error() string { return string(r.Action) + ": " + r.Reason }

// Pending reports a refusal that a person can resolve, as opposed to a final denial.
func (r *Refused) Pending() bool { return r.Action == ActionRequireApproval }

// PolicySubject is WHO is acting, as the policy sees it — resolved server-side.
// Attrs carries verified ABAC attributes (department, clearance, region…) for
// engines that need more than roles. A caller never sets Roles as input to the
// door; the resolver fills them.
type PolicySubject struct {
	ID    string            `json:"id"`
	Roles []Role            `json:"roles"`
	IsAI  bool              `json:"is_ai"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// PolicyIntent is WHAT is being requested, as the policy sees it. Requester and
// Env drive separation-of-duties and generic promote; Params is the open ABAC
// extension (amount, region, time…) engines can read without a contract change.
type PolicyIntent struct {
	Action    string         `json:"action"`
	Resource  string         `json:"resource,omitempty"`
	Requester string         `json:"requester,omitempty"`
	Env       string         `json:"env,omitempty"`
	Goal      string         `json:"goal"`
	Scope     string         `json:"scope,omitempty"` // generic tenancy scope (SDLC maps its "project" here); selects the scope's policy tier
	Params    map[string]any `json:"params,omitempty"`
}

// PolicyInput is the TYPED, versioned port between the door and any policy engine.
// It replaced an untyped map[string]any so a field rename can't silently break a
// downstream policy plugin. Its JSON form is the wire shape a wasm/gRPC policy
// receives — pinned by ContractVersion.
type PolicyInput struct {
	Subject PolicySubject `json:"subject"`
	Intent  PolicyIntent  `json:"intent"`
}

// PolicyEvaluator hides the policy engine. RBAC is the default; OPA/Cedar/Casbin or
// a host's own rules (wasm/gRPC) swap in via config with zero core change. The port
// is TYPED — no engine-specific query string, no untyped map.
type PolicyEvaluator interface {
	Evaluate(ctx context.Context, input PolicyInput) (Decision, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Execution — the TOKEN. A valid intent yields a scoped, expiring token naming what
// was approved and until when. The orchestrator carries it, and a caller that holds
// a token can show WHICH decision authorised the work.
//
// It is not, today, a capability that gates execution: it is an opaque random value,
// unsigned and unverified, so a component that chooses not to check it is unaffected
// by it. Making a token gate execution requires signing it at the door and verifying
// it where the effect happens — and even that binds only components that verify.
// See docs/credential-brokering.md for the structural control.
// ─────────────────────────────────────────────────────────────────────────────

// ExecutionToken records which approved intent authorises a piece of work, and for
// how long. Treat it as EVIDENCE of a decision rather than as a key: it is currently
// unsigned, so it proves nothing to a party that did not issue it.
type ExecutionToken struct {
	Value     string // opaque random value; NOT signed — see the note above
	IntentID  string // the intent it authorises
	Scope     string // resource scope it is valid for
	PolicyID  string // which policy authorised it — surfaced on the wire for audit
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Valid reports whether the token is unexpired at instant t.
func (tk ExecutionToken) Valid(t time.Time) bool {
	return t.Before(tk.ExpiresAt)
}

// ─────────────────────────────────────────────────────────────────────────────
// Workflow — the SPINE. DAG orchestration (petgraph/dominikbraun-graph).
// ─────────────────────────────────────────────────────────────────────────────

// Step is one node in a governed DAG. Runs in WASM (gov code) or a container.
type Step struct {
	Name      string
	DependsOn []string // upstream step names — defines the DAG edges
	Runtime   string   // "builtin" | "wasm" | "container"
	Image     string   // OCI ref for container steps
	Memory    string   // container memory cap (e.g. "512m"); empty → the sandbox default
	Command   []string
	// Compensation is the saga rollback command for this step. If a later step
	// fails, already-completed steps are compensated in reverse order. Empty =
	// nothing to undo. Runs under the same Runtime as the forward step.
	Compensation []string
}

// DAG is an acyclic workflow. Construction should reject cycles.
type DAG struct {
	Name  string
	Steps []Step
}

// RunResult is the outcome of executing a DAG.
type RunResult struct {
	DAGName    string
	Success    bool
	FailedStep string // empty if Success
	StartedAt  time.Time
	FinishedAt time.Time
}

// WorkflowRunner executes a DAG under a valid ExecutionToken. Impl: channel
// transport (embedded, MVP) → NATS coordinator/worker (distributed).
type WorkflowRunner interface {
	Run(ctx context.Context, token ExecutionToken, dag DAG) (RunResult, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Audit — the EVIDENCE. Append-only, hash-chained, tamper-evident.
// Evidence is a byproduct: every decision is captured automatically.
// ─────────────────────────────────────────────────────────────────────────────

// AuditRecord is one immutable entry in the hash-chained log.
type AuditRecord struct {
	Timestamp time.Time
	IntentID  string
	SubjectID string
	Action    string
	Decision  DecisionAction
	PolicyID  string
	IsAI      bool // AI-generated actions are flagged separately
	// Via is the application that asserted SubjectID on someone's behalf; empty when
	// the subject was the authenticated caller. `omitempty` is load-bearing: the hash
	// is taken over the marshalled record, so an absent Via must serialise to exactly
	// the bytes it did before this field existed, or every pre-existing record in the
	// chain would fail verification.
	Via      string `json:"Via,omitempty"`
	PrevHash string // SHA-256 of the previous record — the chain link
	Hash     string // SHA-256 of this record (set by the logger)
}

// AuditLogger appends decisions to the immutable trail. Impl: bbolt (MVP) →
// ClickHouse / S3 Object Lock (7-year WORM, production).
type AuditLogger interface {
	Log(ctx context.Context, rec AuditRecord) (AuditRecord, error) // returns rec with Hash set
	Verify(ctx context.Context) (bool, error)                      // walk the chain, detect tampering
}

// ─────────────────────────────────────────────────────────────────────────────
// Store — the DNA. Generic persistence. Dual mode via the same interface.
// ─────────────────────────────────────────────────────────────────────────────

// Store is generic key/value persistence. Default: bbolt (embedded, pure-Go, dependency-free). A
// deployable needing PostgreSQL compiles that adapter INTO ITSELF, behind this same interface —
// its driver deps stay in that binary (CVE-isolated), never in the generic core library.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, value []byte) error
	List(ctx context.Context, prefix string) ([]string, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// The one door. Everything flows through Submit. Assembled from the above.
// ─────────────────────────────────────────────────────────────────────────────

// Submitter is the single public entrypoint — humans and AI both call this.
// Implemented by the SDK, which wires Identity → Policy → Audit → token issue.
type Submitter interface {
	// Submit validates the intent, evaluates policy, records the decision, and
	// returns an ExecutionToken on allow. Returns an error carrying the Decision
	// on deny or require_approval.
	Submit(ctx context.Context, intent Intent) (ExecutionToken, error)
}
