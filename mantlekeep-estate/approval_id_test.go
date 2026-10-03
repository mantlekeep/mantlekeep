package estate

import (
	"context"
	"errors"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// The defect, end to end, on a clock that cannot tell two instants apart.
//
// Approving a gated change submits it to the door again as the approver; a door that requires
// approval again makes the manager open a second request for the same change. On a coarse clock
// that second request is created at the same instant as the first. If the two share an id, the
// second replaces the decided first, and the next approval of that id is refused as a
// SELF-approval — the two-party rule firing against the approver. Freezing the clock reproduces a
// coarse one deterministically instead of waiting for a host that has it.
func TestAFrozenClockStillApprovesOnce(t *testing.T) {
	// Frozen at the present, not at a fixed date: the store judges expiry by its own clock, and
	// a fixed date would eventually open approvals that have already expired.
	frozen := time.Now().UTC()
	manager, store, _ := gatedManager(t)
	manager.now = func() time.Time { return frozen }

	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	if err != nil || len(outcome.Refused) == 0 {
		t.Fatalf("a prod change must be refused pending a person; refused=%d err=%v",
			len(outcome.Refused), err)
	}
	id := outcome.Refused[0].Approval
	approver := mantlekeep.Subject{ID: "lead-bob"}

	if _, err := manager.Approve(context.Background(), approver, id); err != nil {
		t.Fatalf("first approval: %v", err)
	}
	decided, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("reading the approval back: %v", err)
	}
	if decided.State != ApprovalApproved || decided.ApprovedBy != "lead-bob" ||
		decided.Requester != "dev-alice" {
		t.Errorf("the decided approval was replaced: state=%q approvedBy=%q requester=%q — the "+
			"record of who approved this change is gone",
			decided.State, decided.ApprovedBy, decided.Requester)
	}

	if _, second := manager.Approve(context.Background(), approver, id); !errors.Is(second,
		ErrApprovalNotPending) {
		t.Errorf("a second approval of a decided change must be refused as not pending; got %v "+
			"(self-approval: %v)", second, errors.Is(second, ErrSelfApproval))
	}
}

// Two approvals for the SAME change at the SAME instant must still get different ids.
//
// The store refuses an id it already holds, but a refusal is still a failure: with a colliding id
// the second, legitimate request cannot be opened at all. The id has to be unique on its own.
func TestTwoApprovalsAtOneInstantGetDifferentIDs(t *testing.T) {
	at := time.Now().UTC()

	first := approvalID("payments", "payments-api", at)
	second := approvalID("payments", "payments-api", at)

	if first == second {
		t.Errorf("two approvals created at the same instant share an id: %s — the id depends on "+
			"how finely the clock ticks", first)
	}
}
