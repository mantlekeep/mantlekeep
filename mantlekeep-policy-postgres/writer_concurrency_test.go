package pgpolicy

// What must be true when two operators change policy at the same moment. Kept in its own file
// because it is the property the store's whole shape is chosen for, and it should not have to
// be found among the tests of what a single change does.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// TestAConcurrentWriteDoesNotLoseTheOtherOperatorsChange is the concurrency guarantee.
//
// Two operators are editing policy at the same moment. Operator B's change commits in the
// window between operator A's read and A's write — the window a plain read-modify-write
// leaves open. If A's write went through unconditionally it would store a document computed
// from a policy that no longer exists, silently reverting B while reporting success to A.
// Neither operator would ever learn that a permission change they were told had been applied
// was not.
//
// Both changes must survive, and the history must show both.
func TestAConcurrentWriteDoesNotLoseTheOtherOperatorsChange(t *testing.T) {
	store := &fakeStore{}
	store.seed(t, grantsDoc(map[string][]string{"L2-Operator": {"job.run"}}), floorsDoc(nil))

	operatorB := grants.Change{
		Role: "L3-Approver", Action: "deploy.prod", Grant: true, Reason: "quarterly release window"}

	// Operator B commits after operator A has read the head and before A applies.
	store.raceBeforeCommit = func(racing *fakeStore) {
		if _, err := New(racing).Write(context.Background(), operatorB); err != nil {
			t.Errorf("operator B's write failed: %v", err)
		}
	}

	operatorA := grants.Change{
		Role: "L2-Operator", Action: "deploy.dev", Grant: true, Reason: "onboarding the new starter"}

	revision, err := New(store).Write(context.Background(), operatorA)
	if err != nil {
		t.Fatalf("operator A's write failed after losing the race; a change the door approved "+
			"was thrown away because somebody else was quicker: %v", err)
	}

	got := store.roleActions(t)
	want := map[string][]string{
		"L2-Operator": {"job.run", "deploy.dev"}, // operator A
		"L3-Approver": {"deploy.prod"},           // operator B — must still be there
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a concurrent change was lost:\n  got:  %v\n  want: %v", got, want)
	}

	// Both changes on the record, in the order they landed, each with its reason.
	if len(store.history) != 2 {
		t.Fatalf("history holds %d changes, want 2 — a change that was applied and not "+
			"recorded is one no audit can find", len(store.history))
	}
	if store.history[0].Change != operatorB || store.history[1].Change != operatorA {
		t.Errorf("history does not show both changes in the order they landed: %+v", store.history)
	}
	// The chain the history records must actually join up: A's parent is the revision B left.
	if store.history[1].ParentRevision != store.history[0].Revision() {
		t.Errorf("the history is not a chain: %q was applied to %q, but the previous entry "+
			"produced %q", store.history[1].Revision(), store.history[1].ParentRevision,
			store.history[0].Revision())
	}
	if revision != store.head.StoredRevision {
		t.Errorf("Write reported revision %q but the store holds %q", revision, store.head.StoredRevision)
	}
}

// Revision is what this entry's documents carry — a small helper so the test above reads as
// the property it is asserting rather than as field access.
func (e Entry) Revision() grants.Revision { return e.StoredRevision }

// TestWriteGivesUpRatherThanHangingOnAPermanentConflict.
//
// Retrying is right when the conflict is another operator; it is wrong when the conflict never
// clears, because an unbounded retry turns a governed change into one that hangs. A hung
// change is worse than a failed one: the approval is spent and nobody is told.
func TestWriteGivesUpRatherThanHangingOnAPermanentConflict(t *testing.T) {
	store := &alwaysConflictingStore{}

	_, err := New(store).Write(context.Background(), grants.Change{
		Role: "L2-Operator", Action: "deploy.dev", Grant: true, Reason: "onboarding"})

	if err == nil {
		t.Fatal("Write reported success against a store that never accepted the change")
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("the error does not say what went wrong: %v", err)
	}
	// Bounded against LITERALS, not against maxWriteAttempts: an assertion written in terms
	// of the constant it is guarding moves whenever the constant does and would pass at any
	// value, including one large enough to make a governed change hang.
	const (
		mustRetryAtLeast = 2  // fewer, and it never re-read after a conflict at all
		mustStopBy       = 10 // more, and the change is hanging rather than failing
	)
	if store.updateCalls < mustRetryAtLeast {
		t.Errorf("Write made %d attempts — it gave up without re-reading, so the loser of a "+
			"race loses a change the door approved", store.updateCalls)
	}
	if store.updateCalls > mustStopBy {
		t.Errorf("Write made %d attempts — an effectively unbounded retry is a governed change "+
			"that hangs instead of failing, with the approval spent and nobody told", store.updateCalls)
	}
}

// alwaysConflictingStore is a head that is never the one that was read — the conflict that
// does not clear.
type alwaysConflictingStore struct {
	updateCalls int
}

func (s *alwaysConflictingStore) Head(context.Context) (Snapshot, error) {
	return Snapshot{
		GrantsDoc:      []byte(`{"role_actions":{}}`),
		FloorsDoc:      []byte(`{"floors":{}}`),
		StoredRevision: "aaaaaaaaaaaaaaaa",
	}, nil
}

func (s *alwaysConflictingStore) Update(context.Context, func(Snapshot) (Entry, error)) error {
	s.updateCalls++
	return ErrRevisionConflict
}
