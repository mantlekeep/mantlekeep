package doorclient_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// The require_approval path, through the REAL door.
//
// # Why this test exists separately from the allow and deny cases
//
// require_approval carries the most wire surface of any outcome: the word itself, the 409, and
// requiredApprovers — a field neither of the other outcomes sends. It is therefore where a
// field-name mismatch hides longest, and one already did: the client read "decision" where the
// door sends "outcome", and every test on both sides passed for as long as nobody ran the pair.
//
// It is also the outcome with the worst failure mode. A mis-read allow is a puzzling error. A
// mis-read require_approval tells somebody their change was REFUSED when in fact it is waiting
// for a colleague to sign — so they go looking for a way around instead of asking for a signature.
func TestARequireApprovalSurvivesTheRealDoor(t *testing.T) {
	gateProductionApprovals(t)

	server, client := realDoorAndClient(t)
	defer server.Close()

	_, err := client.Submit(context.Background(), mantlekeep.Intent{
		ID: "AP-1", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "a change a second person must sign"},
		Params:  map[string]any{"env": "prod"},
	})
	if err == nil {
		t.Fatal("a gated change must not come back as a plain allow")
	}

	decision, carried := mantlekeep.DecisionFrom(err)
	if !carried {
		t.Fatalf("the outcome must survive the wire as a decision, not as a broken door: %v", err)
	}

	// The word itself. This is the assertion the old field names silently failed.
	if decision.Action != mantlekeep.ActionRequireApproval {
		t.Fatalf("a gated change is WAITING for a person, not refused; got %q", decision.Action)
	}

	// AwaitingApproval is what a surface branches on to render "waiting" rather than "denied".
	if !mantlekeep.AwaitingApproval(err) {
		t.Fatal("AwaitingApproval must recognise this, or every surface renders it as a failure")
	}

	// A refusal that cannot say who unblocks it is a dead end wearing the shape of a process.
	if len(decision.RequiredApprovers) == 0 {
		t.Fatalf("requiredApprovers must survive the wire — without it nobody knows who to ask: %+v", decision)
	}

	// The reason must reach the person, in words. The door sends an ARRAY of coded reasons; a
	// client that read a string field would arrive here with an empty message.
	if decision.Reason == "" {
		t.Fatal("the reason must cross the wire — a silent refusal sends people to find whoever they can")
	}
}

// A deny and a require_approval must not read the same to a caller.
//
// Both are non-allow, both arrive as an error. Only one of them is somebody's fault. If a client
// collapses them, an operator cannot tell "you may not" from "not yet".
func TestADenyAndARequireApprovalAreDistinguishable(t *testing.T) {
	gateProductionApprovals(t)

	server, client := realDoorAndClient(t)
	defer server.Close()

	_, gated := client.Submit(context.Background(), mantlekeep.Intent{
		ID: "AP-2", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "gated"},
		Params:  map[string]any{"env": "prod"},
	})
	_, denied := client.Submit(context.Background(), mantlekeep.Intent{
		ID: "AP-3", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "nobody"},
		Spec:    mantlekeep.IntentSpec{Goal: "no role at all"},
	})

	if gated == nil || denied == nil {
		t.Fatal("both must be refused")
	}
	if mantlekeep.AwaitingApproval(denied) {
		t.Fatal("a plain denial must NOT read as waiting for a signature")
	}
	if !mantlekeep.AwaitingApproval(gated) {
		t.Fatal("a gated change must read as waiting, not as a denial")
	}
}

// gateProductionApprovals points the engine at a floors document that gates prod changes.
//
// The shipped document is empty — {"floors":{}} — so nothing is approval-gated by default, which
// is exactly why this path went untested. The gate is DATA, not code: a deployment decides what
// needs a second signature.
func gateProductionApprovals(t *testing.T) {
	t.Helper()

	document := filepath.Join(t.TempDir(), "floors.json")
	content := `{"floors":{"job.run":[{
	  "kind":"require_approval_when",
	  "whenParam":"env",
	  "whenValue":"prod",
	  "role":"operator",
	  "message":"a production change requires approval by a second person"
	}]}}`
	if err := os.WriteFile(document, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the floors document: %v", err)
	}
	t.Setenv("MANTLEKEEP_POLICY_FLOORS", document)
}
