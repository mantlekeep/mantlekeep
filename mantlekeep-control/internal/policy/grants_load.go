package policy

import "github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"

// This file is the READ side of the policy documents: the role→action grants, the approval
// actions, and the attribute floors, as loaded from the shared documents
// (github.com/mantlekeep/mantlekeep/mantlekeep-control/grants). The vocabulary lives in the DATA (the core baseline + each
// product's doc, merged), read here — so rbac.go/floor.go stay grep-clean of product names.
// The OPA adapter reads the SAME merged documents, so the two engines cannot drift.
//
// Each reader takes ONE atomic load of the snapshot in force (see grants_live.go). Reading a
// snapshot rather than a package-level map is what allows the documents to be replaced under
// running traffic: an evaluation in flight finishes against the policy it started on, and the
// next one sees the new policy — never a half of each.

// EnsureLoaded eagerly loads + validates the merged policy (baseline ∪ platform ∪ products),
// including the platform SEAL. Call it at door startup so a misconfigured policy — e.g. a
// product doc granting a sealed platform action — FAILS FAST at boot, not on the first user
// request. Idempotent; the readers below reuse the same snapshot.
func EnsureLoaded() { ensurePolicy() }

// roleActions is the merged role→action map (baseline ∪ products) currently in force.
func roleActions() map[string]map[string]bool { return ensurePolicy().roleActions }

// approvalActions is the merged AI-cannot-approve set currently in force.
func approvalActions() map[string]bool { return ensurePolicy().approvalActions }

// floors is the merged attribute-floor document currently in force.
func floors() *grants.Floors { return ensurePolicy().floors }
