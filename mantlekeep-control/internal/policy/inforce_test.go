package policy

import (
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// InForce hands the live law to a caller that only means to display it. If any part of what it
// returns still points into the engine's snapshot, a surface that edits its own copy — sorting a
// slice, appending a rendered entry, blanking a field — edits the policy the door enforces.
//
// A fresh outer map holding the same rule slices is not a copy, and neither is a fresh rule
// holding the same Values slice, so this reaches all the way down.
func TestWhatInForceReturnsCannotEditThePolicyTheDoorEnforces(t *testing.T) {
	const action = "inforce.detached"
	withFloors(t, &grants.Floors{Floors: map[string][]grants.FloorRule{
		action: {{
			Kind: "allowlist", Param: "app", Values: []string{"approved"},
			WhenIn: []string{"prod"}, Caps: map[string]string{"cpu": "2"},
			Message: "application is not on the register",
		}},
	}})

	_, shown := InForce()
	if len(shown.Floors[action]) != 1 {
		t.Fatalf("expected the seeded rule to come back, got %d", len(shown.Floors[action]))
	}

	// Vandalise every reference type the caller can reach.
	shown.Floors[action][0].Values[0] = "anything-goes"
	shown.Floors[action][0].WhenIn[0] = "anywhere"
	shown.Floors[action][0].Caps["cpu"] = "unlimited"
	shown.Floors[action][0].Message = "overwritten"
	shown.Floors[action] = nil
	shown.Floors["inforce.invented"] = []grants.FloorRule{{Kind: "allowlist"}}

	enforced := floors()
	if len(enforced.Floors[action]) != 1 {
		t.Fatalf("the engine's floor for %q is now %d rules — the caller was handed the "+
			"engine's own map", action, len(enforced.Floors[action]))
	}
	rule := enforced.Floors[action][0]
	switch {
	case rule.Values[0] != "approved":
		t.Fatalf("the allowlist the door enforces now reads %q — a caller edited the law",
			rule.Values[0])
	case rule.WhenIn[0] != "prod":
		t.Fatalf("the rule's condition now reads %q — a caller edited the law", rule.WhenIn[0])
	case rule.Caps["cpu"] != "2":
		t.Fatalf("the rule's cap now reads %q — a caller edited the law", rule.Caps["cpu"])
	case rule.Message == "overwritten":
		t.Fatal("the refusal message the door gives is now the caller's text")
	}
	if _, invented := enforced.Floors["inforce.invented"]; invented {
		t.Fatal("a caller added a rule to the policy the door enforces")
	}
}
