package policy

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// This file makes the GRANT and FLOOR documents reloadable while the door is running.
//
// WHY: the engine used to load them once (sync.Once) and hold them for the life of the
// process. Onboarding one application — adding its name to an allowlist floor, granting a
// team an action — therefore meant a redeploy, which under change control means a raised request
// and a change window. Governance that can only be changed on a release cadence is governance
// that gets bypassed in an incident, and a bypassed guardrail governs nothing.
//
// It is the same mechanism dynamic.go already uses for the layered config, for the same
// reason, and deliberately the same shape so there is one thing to understand rather than two:
//
//  1. The active documents live in ONE immutable snapshot behind an atomic pointer. Readers
//     take a single atomic load — no lock on the decision path, and no reader can observe a
//     half-applied policy, because a snapshot is never edited in place, only replaced.
//  2. A reload BUILDS the whole new snapshot first and swaps only if it built. A malformed
//     document, a product doc granting a sealed platform action, an unreadable file — each
//     fails during the build, before the swap, so the last good policy keeps deciding.
//  3. Every reload runs the SAME merge and the SAME seal as boot, because it calls the same
//     loader. Hot-reload does not open a second, laxer path into the policy: a document that
//     boot would have rejected is rejected here too.
//
// # Why boot still panics and a reload never does
//
// They are different failures. At boot nothing is serving yet, so a bad document means this
// process must not start — failing fast is the only honest answer, and it is what door.go's
// EnsureLoaded relies on. During a reload something IS serving, and it is serving policy that
// was valid. Tearing that down because someone saved a broken file would turn a typo into an
// outage, and — worse — an outage of the component that says no.

// grantsSnapshot is one fully-built, immutable view of the policy documents in force.
//
// Built once per reload and never mutated afterwards. That is what lets readers hold it
// without a lock: the pointer moves, the contents never do.
type grantsSnapshot struct {
	roleActions     map[string]map[string]bool
	approvalActions map[string]bool
	floors          *grants.Floors
	revision        grants.Revision
}

var (
	// livePolicy holds the snapshot the engine is deciding against. nil until first use.
	livePolicy atomic.Pointer[grantsSnapshot]
	// seedOnce guards the LAZY first load only. Loading stays lazy — a MANTLEKEEP_POLICY_DIR set
	// before the first governed call, including by a test's TestMain, must still be honoured.
	// Reloads do not go through it; they build and swap.
	seedOnce sync.Once
)

// ensurePolicy returns the snapshot in force, seeding it from the configured documents on
// first use. A malformed document panics here, and only here — see the note above.
func ensurePolicy() *grantsSnapshot {
	seedOnce.Do(func() {
		snapshot, err := buildSnapshot(grants.MustLoad(), grants.MustLoadFloors())
		if err != nil {
			panic(err)
		}
		livePolicy.Store(snapshot)
	})
	return livePolicy.Load()
}

// buildSnapshot turns loaded documents into the decision-shaped maps the engine reads.
//
// It takes the documents rather than loading them so that the boot path and the reload path
// build the identical shape from the identical input — the reload cannot drift into a
// different interpretation of the same file.
func buildSnapshot(held *grants.Grants, floors *grants.Floors) (*grantsSnapshot, error) {
	if held == nil || floors == nil {
		// Empty grants deny everything, so a nil document and a document that legitimately
		// grants nothing would be indistinguishable — and the first would install itself as a
		// working deny-all rather than announce that a load failed.
		return nil, fmt.Errorf("policy snapshot: documents must not be nil")
	}
	// The generic L0-SuperAdmin wildcard is the one grant that is NOT product policy; it lives
	// in the engine. Every other role's EXPLICIT action set comes from the merged documents.
	roleActions := map[string]map[string]bool{"L0-SuperAdmin": {"*": true}}
	for role, actions := range held.RoleActions {
		roleActions[role] = set(actions...)
	}
	approvalActions := make(map[string]bool, len(held.ApprovalActions))
	for _, action := range held.ApprovalActions {
		approvalActions[action] = true
	}
	return &grantsSnapshot{
		roleActions:     roleActions,
		approvalActions: approvalActions,
		floors:          floors,
		revision:        grants.RevisionOfDocuments(held, floors),
	}, nil
}
