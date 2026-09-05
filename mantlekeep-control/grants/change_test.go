package grants

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

type recordingDoor struct {
	submitted []mantlekeep.Intent
	refuse    error
}

func (d *recordingDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.submitted = append(d.submitted, intent)
	if d.refuse != nil {
		return mantlekeep.ExecutionToken{}, d.refuse
	}
	return mantlekeep.ExecutionToken{
		Value: "opaque", IntentID: "INT-1", ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

type recordingWriter struct{ written []Change }

func (w *recordingWriter) Write(_ context.Context, change Change) (Revision, error) {
	w.written = append(w.written, change)
	return "after", nil
}

type fixedSource struct{ revision Revision }

func (s fixedSource) Load(context.Context) (*Grants, *Floors, Revision, error) {
	return &Grants{RoleActions: map[string][]string{}}, &Floors{}, s.revision, nil
}

func aChange() Change {
	return Change{Role: "L2-Operator", Action: "deploy.prod", Grant: true,
		Reason: "on-call rotation now covers prod deploys"}
}

// THE property. A policy change reaches the door before it reaches the policy — otherwise
// somebody who can edit the grants does not need to bypass the door, because they can grant
// themselves the role that opens it.
func TestAPolicyChangeGoesThroughTheDoorFirst(t *testing.T) {
	door, writer := &recordingDoor{}, &recordingWriter{}

	revision, err := Govern(context.Background(), door,
		mantlekeep.Subject{ID: "dev-alice"}, fixedSource{revision: "before"}, writer, aChange())
	if err != nil {
		t.Fatalf("Govern: %v", err)
	}
	if len(door.submitted) != 1 {
		t.Fatalf("the door saw %d intents, want 1", len(door.submitted))
	}
	if door.submitted[0].Action != ActionGrantPolicy {
		t.Errorf("action = %q, want %q", door.submitted[0].Action, ActionGrantPolicy)
	}
	if revision != "after" {
		t.Errorf("revision = %q, want the one the writer reported", revision)
	}
	if len(writer.written) != 1 {
		t.Error("the change did not reach the policy")
	}
}

// A refused change must not be written. This is the whole point: the door decides, then the
// effect runs — never the other way round.
func TestARefusedPolicyChangeIsNotApplied(t *testing.T) {
	door := &recordingDoor{refuse: errors.New("deny: only the platform may grant this action")}
	writer := &recordingWriter{}

	_, err := Govern(context.Background(), door,
		mantlekeep.Subject{ID: "dev-alice"}, fixedSource{}, writer, aChange())
	if err == nil {
		t.Fatal("a refused policy change returned success")
	}
	if len(writer.written) != 0 {
		t.Fatal("a refused change was written to the policy anyway")
	}
	if !strings.Contains(err.Error(), "only the platform") {
		t.Errorf("the door's own words were lost: %v", err)
	}
}

// No door, no change. An ungoverned edit is the thing this replaces, so it must not be
// reachable by leaving an argument nil.
func TestAChangeWithNoDoorIsRefused(t *testing.T) {
	writer := &recordingWriter{}
	if _, err := Govern(context.Background(), nil,
		mantlekeep.Subject{ID: "dev-alice"}, fixedSource{}, writer, aChange()); err == nil {
		t.Fatal("a policy change was applied with no door")
	}
	if len(writer.written) != 0 {
		t.Fatal("the policy was written with no decision behind it")
	}
}

// A permission change nobody explained is one nobody can review a year later.
func TestAChangeWithNoReasonIsRefused(t *testing.T) {
	change := aChange()
	change.Reason = ""
	if _, err := Govern(context.Background(), &recordingDoor{},
		mantlekeep.Subject{ID: "dev-alice"}, fixedSource{}, &recordingWriter{}, change); err == nil {
		t.Fatal("a policy change with no reason was accepted")
	}
}

// The revision in force reaches the intent, so the chain records which policy became which
// rather than merely that something changed.
func TestTheRevisionInForceReachesTheChain(t *testing.T) {
	door := &recordingDoor{}
	if _, err := Govern(context.Background(), door, mantlekeep.Subject{ID: "dev-alice"},
		fixedSource{revision: "rev-abc"}, &recordingWriter{}, aChange()); err != nil {
		t.Fatal(err)
	}
	if got, _ := door.submitted[0].Params["revision"].(string); got != "rev-abc" {
		t.Errorf("revision on the intent = %q, want rev-abc", got)
	}
}

// An approver must read the change, not a file. "policy updated" is not a decision anybody
// can take responsibility for.
func TestTheGoalDescribesTheActualChange(t *testing.T) {
	door := &recordingDoor{}
	if _, err := Govern(context.Background(), door, mantlekeep.Subject{ID: "dev-alice"},
		fixedSource{}, &recordingWriter{}, aChange()); err != nil {
		t.Fatal(err)
	}
	goal := door.submitted[0].Spec.Goal
	for _, want := range []string{"deploy.prod", "L2-Operator", "on-call rotation"} {
		if !strings.Contains(goal, want) {
			t.Errorf("the goal %q does not mention %q", goal, want)
		}
	}
}

// A whole-document edit becomes reviewable changes, one per alteration.
func TestADocumentEditBecomesReviewableChanges(t *testing.T) {
	before := &Grants{RoleActions: map[string][]string{"L2-Operator": {"deploy.dev"}}}
	after := &Grants{RoleActions: map[string][]string{"L2-Operator": {"deploy.dev", "deploy.prod"}}}

	changes := ChangesFrom(before, after, "quarterly review")
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(changes), changes)
	}
	if !changes[0].Grant || changes[0].Action != "deploy.prod" {
		t.Errorf("wrong change: %+v", changes[0])
	}
}

// A revocation is a change too, and the more consequential direction to get wrong.
func TestARevocationIsSeenAsAChange(t *testing.T) {
	before := &Grants{RoleActions: map[string][]string{"L2-Operator": {"deploy.dev", "deploy.prod"}}}
	after := &Grants{RoleActions: map[string][]string{"L2-Operator": {"deploy.dev"}}}

	changes := ChangesFrom(before, after, "access review")
	if len(changes) != 1 || changes[0].Grant {
		t.Fatalf("a revocation was not reported: %+v", changes)
	}
}

// The same policy always produces the same revision, however it was authored or stored —
// which is what lets a file deployment and a database deployment be proved equivalent.
func TestTheRevisionIsContentAddressed(t *testing.T) {
	// Assigned rather than compared inline: the point is that two SEPARATE derivations of
	// the same content agree, which is what makes a revision comparable across processes.
	first := RevisionOf([]byte("a"), []byte("b"))
	second := RevisionOf([]byte("a"), []byte("b"))
	if first != second {
		t.Error("identical content produced different revisions")
	}
	if first == RevisionOf([]byte("ab")) {
		t.Error("two documents concatenated into one shared a revision — length prefixing failed")
	}
	if RevisionOf([]byte("a")) == RevisionOf([]byte("b")) {
		t.Error("different content shared a revision")
	}
}
