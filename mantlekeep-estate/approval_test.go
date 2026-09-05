package estate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// pendingDoor refuses everything with require_approval, naming who may sign off — the shape the
// door emits for a gated change.
type pendingDoor struct{ submitted []mantlekeep.Intent }

func (d *pendingDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.submitted = append(d.submitted, intent)
	if intent.Params["gate"] == string(GateNone) {
		return mantlekeep.ExecutionToken{Value: "tok", IntentID: intent.ID,
			ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	return mantlekeep.ExecutionToken{}, &mantlekeep.Refused{
		Action: mantlekeep.ActionRequireApproval, Reason: "a platform approver must sign off",
		RequiredApprovers: []mantlekeep.Role{"L1-Architect"},
	}
}

func gatedManager(t *testing.T) (*Manager, *MemoryApprovals, *recordingPort) {
	t.Helper()
	port := &recordingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	return NewManager(&pendingDoor{}, DefaultFloor(), port).AwaitApprovalIn(store), store, port
}

func gatedManifest(t *testing.T) Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"prod",
	  "kafka":{"topics":["settlements"]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

// The gap this closes. The door could say "a person is needed" and nothing could act on it — a
// gate in name only.
func TestAGatedChangeLeavesSomethingAPersonCanActOn(t *testing.T) {
	manager, store, _ := gatedManager(t)
	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Refused) == 0 {
		t.Fatal("a prod change must be refused pending a person")
	}
	if outcome.Refused[0].Approval == "" {
		t.Fatal("refused with no approval reference — the caller has nobody to ask and nothing " +
			"to poll, which is a dead end wearing the shape of a process")
	}

	// One approval per gated CHANGE, not per manifest — the same reason the door is submitted
	// per change: a person approves a thing, not a document.
	waiting, err := store.Pending(context.Background(), "payments")
	if err != nil || len(waiting) != len(outcome.Refused) {
		t.Fatalf("every gated change must be recorded; got %d for %d refusals (%v)",
			len(waiting), len(outcome.Refused), err)
	}
	recorded, err := store.Get(context.Background(), outcome.Refused[0].Approval)
	if err != nil {
		t.Fatalf("the referenced approval must exist: %v", err)
	}
	if recorded.Requester != "dev-alice" {
		t.Errorf("requester = %q — separation of duties needs both names to compare",
			recorded.Requester)
	}
	if len(recorded.RequiredRoles) != 1 || recorded.RequiredRoles[0] != "L1-Architect" {
		t.Errorf("required roles = %v — a refusal that cannot say who unblocks it is a dead end",
			recorded.RequiredRoles)
	}
}

// THE floor. A two-party rule satisfied by one party is not a rule, and no configuration
// reaches this.
func TestTheRequesterCannotApproveTheirOwnChange(t *testing.T) {
	manager, store, port := gatedManager(t)
	outcome, _ := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	id := outcome.Refused[0].Approval

	_, err := manager.Approve(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, id)
	if !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self-approval must be refused in CODE, not by convention; got %v", err)
	}
	if len(port.tokens) != 0 {
		t.Fatal("the change reached the adapter on a self-approval")
	}
	still, err := store.Get(context.Background(), id)
	if err != nil || !still.Pending(time.Now()) {
		t.Errorf("a refused self-approval must leave the request pending for somebody else; "+
			"state=%q err=%v", still.State, err)
	}
}

// The ROLE check is the door's, not the estate's — caller.go: "a caller that could assert its own
// roles could assert its way past any gate". So there is deliberately no test here for an
// approver holding the wrong role; submitting as the approver is what puts that question to the
// door, and the door's floor rule answers it.
// The floor is hot-reloadable, so the rules can move between the request and the signature.

func TestTheFloorMustNotHaveMovedSinceTheRequest(t *testing.T) {
	floor := DefaultFloor()
	floor.Revision = "at-request-time"
	port := &recordingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	manager := NewManager(&pendingDoor{}, floor, port).
		AwaitApprovalIn(store).
		FloorFrom(func() Floor { return floor })

	outcome, _ := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	id := outcome.Refused[0].Approval

	// An operator edits the floor and reloads before anybody signs off.
	floor.Revision = "after-a-reload"

	_, err := manager.Approve(context.Background(),
		mantlekeep.Subject{ID: "arch-carol"}, id)
	if !errors.Is(err, ErrFloorMoved) {
		t.Fatalf("what would be applied is not what was approved; got %v", err)
	}
	if len(port.tokens) != 0 {
		t.Fatal("a change was applied under limits nobody approved")
	}
}

func TestApprovingTwiceAppliesOnce(t *testing.T) {
	manager, _, _ := gatedManager(t)
	outcome, _ := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	id := outcome.Refused[0].Approval
	approver := mantlekeep.Subject{ID: "arch-carol"}

	if _, err := manager.Approve(context.Background(), approver,
		id); err != nil {
		t.Fatalf("first approval: %v", err)
	}
	if _, err := manager.Approve(context.Background(), approver,
		id); !errors.Is(err, ErrApprovalNotPending) {
		t.Fatalf("a second approval must not apply the change again; got %v", err)
	}
}

func TestADeclineNeedsAReason(t *testing.T) {
	manager, _, _ := gatedManager(t)
	outcome, _ := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))

	err := manager.Decline(context.Background(), mantlekeep.Subject{ID: "arch-carol"},
		outcome.Refused[0].Approval, "")
	if err == nil || !strings.Contains(err.Error(), "needs a reason") {
		t.Fatalf("declined with no words sends the requester looking for another route; got %v", err)
	}
}

func TestAnExpiredRequestIsNotPending(t *testing.T) {
	store := NewMemoryApprovals()
	past := time.Now().Add(-time.Hour)
	if err := store.Open(context.Background(), Approval{
		ID: "APR-old", Team: "payments", State: ApprovalPending,
		CreatedAt: past.Add(-time.Hour), ExpiresAt: past,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), "APR-old")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != ApprovalExpired {
		t.Errorf("state = %q — expiry must hold on READ, or a store with no sweeper hands back "+
			"stale requests as live ones", got.State)
	}
	if waiting, _ := store.Pending(context.Background(), ""); len(waiting) != 0 {
		t.Error("an expired request must leave the queue, or people stop reading the queue")
	}
}
