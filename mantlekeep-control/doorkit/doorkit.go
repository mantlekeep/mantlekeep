// Package doorkit assembles a real one-door (identity + pure-Go RBAC policy + audit) and
// opens its companion stores, as PUBLIC core API.
//
// It exists so a downstream product module (e.g. mantlekeep-portal) can build a genuine
// governed door for its own integration tests — or a lightweight embedding — WITHOUT
// importing core's internal packages or the app package. The app package depends on the
// portal, so a portal-module test importing app would form an illegal cross-module cycle.
// doorkit sits BELOW app and imports no product, so it is the portal-independent assembly
// seam: core's internal engine reached through a small, stable public surface.
package doorkit

import (
	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/internal/audit"
	"mantlekeep.dev/control/internal/identity"
	"mantlekeep.dev/control/internal/policy"
	"mantlekeep.dev/control/internal/sdk"
	"mantlekeep.dev/control/internal/store"
	"mantlekeep.dev/control/orchestrator"
)

// Failsafe is the read-only-mode control on an assembled door: trip it and the door serves
// a safe read-only policy; reset restores normal governance. Its method set matches the
// failsafe a portal mounts, so a *Door.Failsafe passes straight into a portal.
type Failsafe interface {
	Trip()
	Reset()
	Tripped() bool
}

// Door is an assembled one-door and the handles a caller wires into a portal or product:
// the Submitter to govern intents, the identity resolver, the hash-chained audit log, and
// the failsafe control.
type Door struct {
	Submitter mantlekeep.Submitter        // the door — POST intents here
	Identity  mantlekeep.IdentityResolver // the (mock) identity resolver, for dev/test
	Audit     mantlekeep.AuditLogger      // the durable, hash-chained audit log
	Failsafe  Failsafe                // the read-only failsafe control
}

// NewInMemoryDoor builds a door with a MOCK identity, pure-Go RBAC policy, and a bbolt
// audit log at auditPath. Pass dyn (e.g. a product registry) to give product actions their
// RunAs authorization — it is folded in as the RBAC's dynamic action layer, the same way
// the real server wires the product registry into the door.
func NewInMemoryDoor(auditPath string, dyn ...policy.ActionAuthorizer) (*Door, error) {
	aud, err := audit.Open(auditPath)
	if err != nil {
		return nil, err
	}
	rbac := policy.NewRBAC()
	if len(dyn) > 0 && dyn[0] != nil {
		rbac = rbac.WithDynamic(dyn[0])
	}
	fs := policy.NewFailsafe(rbac)
	ids := identity.NewMock()
	return &Door{
		Submitter: sdk.New(ids, fs, aud),
		Identity:  ids,
		Audit:     aud,
		Failsafe:  fs,
	}, nil
}

// EnsureLoaded eagerly loads + validates the merged policy (baseline ∪ platform ∪ products) from
// the configured sources (MANTLEKEEP_PLATFORM_POLICY, MANTLEKEEP_POLICY_DIR), failing fast on a malformed
// doc or a sealed-action violation. A downstream module's test (TestMain) calls this after setting
// the policy env, so an assembled door has its grants before the first governed request — without
// importing core's internal policy package.
func EnsureLoaded() { policy.EnsureLoaded() }

// OpenBoltStore opens a durable, bbolt-backed key/value Store at path — the store a loop
// hub (or any component keyed on mantlekeep.Store) persists into. bbolt is file-durable, so a
// fresh process reopening the same path restores what was written.
func OpenBoltStore(path string) (mantlekeep.Store, error) {
	return store.OpenBolt(path)
}

// OpenBoltEvents opens a durable, bbolt-backed workflow EventStore at path — the run-history
// store the orchestration engine writes each step's timeline into, so a run survives a restart.
// Returned as the public orchestrator.EventStore so a product module wires it into the engine
// without importing core's internal store package.
func OpenBoltEvents(path string) (orchestrator.EventStore, error) {
	return store.OpenBoltEvents(path)
}
