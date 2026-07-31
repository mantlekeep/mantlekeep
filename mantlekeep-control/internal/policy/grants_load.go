package policy

import (
	"sync"

	"mantlekeep.dev/control/grants"
)

// This file loads the role→action grants, approval actions, and the attribute floors from the
// shared policy documents (mantlekeep.dev/control/grants). The vocabulary lives in the DATA (the core
// baseline + each product's doc, merged), read here — so rbac.go/floor.go stay grep-clean of
// product names. The OPA adapter reads the SAME merged documents, so the two engines cannot drift.
//
// Loading is LAZY (sync.Once, on first use) rather than at package init, so a MANTLEKEEP_POLICY_DIR (or
// a single-file override) set before the first governed call — including a test's TestMain — is
// honoured. Production sets the env before the binary starts, so nothing changes for it.
var (
	policyOnce           sync.Once
	roleActionsCache     map[string]map[string]bool
	approvalActionsCache map[string]bool
	floorsCache          *grants.Floors
)

func ensurePolicy() {
	policyOnce.Do(func() {
		g := grants.MustLoad()
		// The generic L0-SuperAdmin wildcard is the one grant that is NOT product policy; it lives
		// in the engine. Every other role's EXPLICIT action set comes from the merged documents.
		roleActionsCache = map[string]map[string]bool{"L0-SuperAdmin": {"*": true}}
		for role, acts := range g.RoleActions {
			roleActionsCache[role] = set(acts...)
		}
		approvalActionsCache = make(map[string]bool, len(g.ApprovalActions))
		for _, a := range g.ApprovalActions {
			approvalActionsCache[a] = true
		}
		floorsCache = grants.MustLoadFloors()
	})
}

// EnsureLoaded eagerly loads + validates the merged policy (baseline ∪ platform ∪ products),
// including the platform SEAL. Call it at door startup so a misconfigured policy — e.g. a product
// doc granting a sealed platform action — FAILS FAST at boot (a panic from MustLoad), not on the
// first user request. Idempotent (sync.Once); the lazy readers below reuse the same result.
func EnsureLoaded() { ensurePolicy() }

// roleActions is the merged role→action map (baseline ∪ products), loaded on first use.
func roleActions() map[string]map[string]bool { ensurePolicy(); return roleActionsCache }

// approvalActions is the merged AI-cannot-approve set, loaded on first use.
func approvalActions() map[string]bool { ensurePolicy(); return approvalActionsCache }

// floors is the merged attribute-floor document (baseline ∪ products), loaded on first use.
func floors() *grants.Floors { ensurePolicy(); return floorsCache }
