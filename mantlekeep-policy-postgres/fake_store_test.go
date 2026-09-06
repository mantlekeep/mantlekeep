package pgpolicy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// fakeStore is a stand-in for a policy store that CANNOT hold the head across a
// read-modify-write — the weaker of the two implementations [Store.Update] allows, and the one
// where optimistic concurrency has to do the work on its own.
//
// Modelled that way on purpose. The Postgres store holds a row lock and therefore never
// conflicts, so a fake that copied it would exercise none of the conflict handling. This one
// leaves the window open, so a test can put another operator through it and prove the adapter
// re-reads and re-applies rather than overwriting.
//
// It deliberately does NOT re-derive the revision it is handed: a test can store documents
// whose revision column disagrees with their content, which is what an edit made outside the
// door looks like, and prove the adapter still reports the truth.
//
// It is strict about writing NOTHING on a conflict, because "returns ErrRevisionConflict" and
// "returns ErrRevisionConflict having already clobbered the head" are different behaviours and
// only one of them is the contract.
type fakeStore struct {
	head    Snapshot
	present bool
	history []Entry

	headErr   error // returned instead of the head, to stand in for an unreachable database
	updateErr error // returned instead of updating, for a failure that is not a conflict

	headCalls   int
	updateCalls int

	// raceBeforeCommit runs after the change has been computed and before it is stored. It is
	// how a test makes another operator commit in the window between this writer's read and
	// its write — the window optimistic concurrency exists to close.
	raceBeforeCommit func(*fakeStore)
}

func (f *fakeStore) Head(context.Context) (Snapshot, error) {
	f.headCalls++
	if f.headErr != nil {
		return Snapshot{}, f.headErr
	}
	if !f.present {
		return Snapshot{}, ErrNoPolicy
	}
	return f.head, nil
}

func (f *fakeStore) Update(ctx context.Context, apply func(Snapshot) (Entry, error)) error {
	f.updateCalls++
	if f.updateErr != nil {
		return f.updateErr
	}
	head, err := f.Head(ctx)
	if err != nil {
		return err
	}
	entry, err := apply(head)
	if err != nil {
		return err
	}

	if f.raceBeforeCommit != nil {
		race := f.raceBeforeCommit
		f.raceBeforeCommit = nil // one racing operator, not one per attempt
		race(f)
	}

	if f.head.StoredRevision != head.StoredRevision {
		// Nothing is written. The adapter is entitled to re-read and re-apply only because
		// this is true, so the fake must be strict about it.
		return ErrRevisionConflict
	}
	f.head = entry.Snapshot
	f.history = append(f.history, entry)
	return nil
}

// seed loads the fake with a policy, storing the revision the documents actually carry.
func (f *fakeStore) seed(t *testing.T, policy *grants.Grants, floors *grants.Floors) {
	t.Helper()
	snapshot, _, err := Encode(policy, floors)
	if err != nil {
		t.Fatalf("seeding the fake store: %v", err)
	}
	f.head = snapshot
	f.present = true
}

// roleActions decodes the fake's current head and returns the role→action grants.
func (f *fakeStore) roleActions(t *testing.T) map[string][]string {
	t.Helper()
	var decoded grants.Grants
	if err := json.Unmarshal(f.head.GrantsDoc, &decoded); err != nil {
		t.Fatalf("the fake store holds a grant document that does not parse: %v", err)
	}
	return decoded.RoleActions
}

// grantsDoc is a small grant document to store.
func grantsDoc(roleActions map[string][]string, approvalActions ...string) *grants.Grants {
	return &grants.Grants{RoleActions: roleActions, ApprovalActions: approvalActions}
}

// floorsDoc is a small floor document to store.
func floorsDoc(floors map[string][]grants.FloorRule) *grants.Floors {
	return &grants.Floors{Floors: floors}
}
