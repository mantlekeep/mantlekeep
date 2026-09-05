package estate

import (
	"context"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// These tests drive the WHOLE gated loop against a door that behaves the way the real one does,
// rather than one that refuses unconditionally. That distinction is the point: the manager could
// always open an approval record, and could never produce one, because the submission named the
// submitter as the requester and the door's separation-of-duties rule fired on the wrong call.
//
// The rule the fake below encodes is the real door's, and it is worth stating plainly:
// separation of duties compares the ACTING SUBJECT against the REQUESTER param. A submission has
// no requester — the subject IS the person asking — so sending one makes the two names equal on
// every gated change and refuses it before anybody can approve.

// realisticDoor mirrors the core's decision order: separation of duties first (a floor),
// then the approval gate (data). A gated change with no second party is PENDING; one that names
// a different requester is an approval and proceeds.
type realisticDoor struct{ submitted []mantlekeep.Intent }

func (d *realisticDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.submitted = append(d.submitted, intent)
	requester, _ := intent.Params["requester"].(string)

	// THE floor, and it is checked before anything else can soften it.
	if requester != "" && requester == intent.Subject.ID {
		return mantlekeep.ExecutionToken{}, &mantlekeep.Refused{
			Action: mantlekeep.ActionDeny,
			Reason: "separation of duties: the approver cannot be the requester",
		}
	}
	// The gate: a consequence-bearing change needs a second party, and one is present only when
	// the caller names a requester other than itself.
	if intent.Params["gate"] != string(GateNone) && requester == "" {
		return mantlekeep.ExecutionToken{}, &mantlekeep.Refused{
			Action:            mantlekeep.ActionRequireApproval,
			Reason:            "a shared-tier change needs a second person",
			RequiredApprovers: []mantlekeep.Role{mantlekeep.RoleOperator},
		}
	}
	return mantlekeep.ExecutionToken{Value: "tok-" + intent.ID, IntentID: intent.ID,
		ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (d *realisticDoor) lastIntent(t *testing.T) mantlekeep.Intent {
	t.Helper()
	if len(d.submitted) == 0 {
		t.Fatal("nothing was submitted to the door")
	}
	return d.submitted[len(d.submitted)-1]
}

func loopManager(t *testing.T) (*Manager, *realisticDoor, *MemoryApprovals, *recordingPort) {
	t.Helper()
	door := &realisticDoor{}
	port := &recordingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	return NewManager(door, DefaultFloor(), port).AwaitApprovalIn(store), door, store, port
}

// Defect 1, stated as a property: a SUBMISSION must never claim to be its own approval.
//
// Naming the submitter as the requester made subject == requester on every gated change, so the
// door's separation-of-duties rule — a rule about APPROVAL — fired on the request. The result
// was a flat denial with no approval reference: a gate nobody could ever pass.
func TestASubmissionNeverClaimsToBeItsOwnApproval(t *testing.T) {
	manager, door, _, _ := loopManager(t)

	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if requester := door.lastIntent(t).Params["requester"]; requester != "" {
		t.Fatalf("the submission carried requester %q while acting as %q — separation of "+
			"duties then compares a person with themselves and refuses the request before "+
			"anybody could approve it", requester, "dev-alice")
	}
	if len(outcome.Refused) == 0 {
		t.Fatalf("a gated change was not refused pending a person: %+v", outcome)
	}
	if !strings.HasPrefix(outcome.Refused[0].Refused, string(mantlekeep.ActionRequireApproval)) {
		t.Fatalf("the refusal reads %q — a change waiting for a person was reported as "+
			"forbidden, and nobody can be asked to unblock something that is denied",
			outcome.Refused[0].Refused)
	}
	if outcome.Refused[0].Approval == "" {
		t.Fatal("refused with no approval reference — the caller has nothing to poll and " +
			"nobody to ask")
	}
}

// The loop, end to end: one person asks, a different person signs, the change reaches the asset.
func TestAGatedChangeCompletesWhenASecondPersonApproves(t *testing.T) {
	manager, door, store, port := loopManager(t)

	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Refused) == 0 || outcome.Refused[0].Approval == "" {
		t.Fatalf("no approval was opened to act on: %+v", outcome)
	}
	id := outcome.Refused[0].Approval
	if len(port.tokens) != 0 {
		t.Fatal("a gated change reached the adapter before anybody approved it")
	}

	result, err := manager.Approve(context.Background(), mantlekeep.Subject{ID: "lead-bob"}, id)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !result.Applied() {
		t.Fatalf("the approved change did not reach the asset: refused=%q failed=%q",
			result.Refused, result.Failed)
	}
	if len(port.tokens) != 1 {
		t.Fatalf("the adapter was handed %d changes, want the one that was approved", len(port.tokens))
	}

	// The APPROVAL submission is where the requester belongs, and it is the only place the
	// door can meaningfully ask "is this approver the person who asked for it?".
	approval := door.lastIntent(t)
	if approval.Subject.ID != "lead-bob" {
		t.Errorf("submitted as %q — an approval submitted as the requester puts one name on a "+
			"two-party decision", approval.Subject.ID)
	}
	if approval.Params["requester"] != "dev-alice" {
		t.Errorf("the approval carried requester %v, want the person who asked — without it "+
			"the door compares the approver against a value nobody supplied and every "+
			"self-approval passes while the code looks like it is checking",
			approval.Params["requester"])
	}

	recorded, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if recorded.State != ApprovalApproved || recorded.ApprovedBy != "lead-bob" {
		t.Errorf("record says state=%q approvedBy=%q", recorded.State, recorded.ApprovedBy)
	}
}

// The floor, proved against a door that WOULD have allowed a second party: the estate refuses
// in code before the door is ever asked, so no policy and no door bug can let one person be
// both parties.
func TestTheRequesterIsRefusedByTheEstateBeforeTheDoorIsAsked(t *testing.T) {
	manager, door, _, port := loopManager(t)

	outcome, _ := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	id := outcome.Refused[0].Approval
	submissions := len(door.submitted)

	if _, err := manager.Approve(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, id); err != ErrSelfApproval {
		t.Fatalf("self-approval must be refused in CODE, not by convention; got %v", err)
	}
	if len(door.submitted) != submissions {
		t.Error("a self-approval reached the door — the floor must not depend on policy data")
	}
	if len(port.tokens) != 0 {
		t.Fatal("the change reached the adapter on a self-approval")
	}
}

// An UNGATED change must be entirely unaffected: no requester, no approval, applied at once.
// This is the over-gating guard — a playground change that suddenly needed a signature would
// teach people to route around the door.
func TestAnUngatedChangeStillAppliesImmediately(t *testing.T) {
	manager, door, _, port := loopManager(t)

	manifest, err := ParseManifest([]byte(
		`{"team":"payments","owns":"payments","tier":"dev","kafka":{"topics":["orders"]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outcome, err := manager.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"}, manifest)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Applied) == 0 || len(outcome.Refused) != 0 {
		t.Fatalf("an ungated change did not apply straight through: %+v", outcome)
	}
	if requester := door.lastIntent(t).Params["requester"]; requester != "" {
		t.Errorf("an ungated change carried requester %q — there is no approval for a "+
			"requester to be separate from", requester)
	}
	if len(port.tokens) == 0 {
		t.Error("the change never reached the adapter")
	}
}
