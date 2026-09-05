package policy

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// withFloors installs floor data for one test and restores whatever was there.
func withFloors(t *testing.T, data *grants.Floors) {
	t.Helper()
	// Let the real load run first, so sync.Once is spent and cannot overwrite the swap.
	ensurePolicy()
	previous := floorsCache
	floorsCache = data
	t.Cleanup(func() { floorsCache = previous })
}

// The gate turns an otherwise-allowed action into a wait for a second person, driven by
// DATA. The engine knows no action, environment or role name of its own.
func TestAPolicyCanRequireApprovalFromData(t *testing.T) {
	withFloors(t, &grants.Floors{Floors: map[string][]grants.FloorRule{
		"widget.build": {{
			Kind:      "require_approval_when",
			WhenParam: "env",
			WhenValue: "prod",
			Role:      "L2-Operator",
			Message:   "a prod change needs a second person",
		}},
	}})

	required, reason, approvers := approvalGate("widget.build", "", "dev-alice",
		map[string]any{"env": "prod"})
	if !required {
		t.Fatal("a rule that matched did not require approval")
	}
	if reason != "a prod change needs a second person" {
		t.Errorf("reason = %q — the data's own words must reach the requester", reason)
	}
	if len(approvers) != 1 || approvers[0] != mantlekeep.Role("L2-Operator") {
		t.Errorf("approvers = %v; a refusal that cannot say who unblocks it is a dead end", approvers)
	}
}

// A change the rule does not match is untouched. The gate must not become a blanket.
func TestTheGateIsSilentWhenItsConditionIsAbsent(t *testing.T) {
	withFloors(t, &grants.Floors{Floors: map[string][]grants.FloorRule{
		"widget.build": {{Kind: "require_approval_when", WhenParam: "env", WhenValue: "prod"}},
	}})
	if required, _, _ := approvalGate("widget.build", "", "dev-alice",
		map[string]any{"env": "dev"}); required {
		t.Fatal("a dev change was gated by a prod rule")
	}
}

// THE termination property. An approval carries a requester who is not the approver, and
// must pass — otherwise approving would open another approval, forever.
func TestASecondPartySatisfiesTheGate(t *testing.T) {
	withFloors(t, &grants.Floors{Floors: map[string][]grants.FloorRule{
		"widget.build": {{Kind: "require_approval_when", WhenParam: "env", WhenValue: "prod"}},
	}})
	if required, _, _ := approvalGate("widget.build", "dev-alice", "arch-carol",
		map[string]any{"env": "prod"}); required {
		t.Fatal("a second person's approval opened another approval — this never terminates")
	}
}

// A policy with no such rule behaves exactly as before. The change is additive.
func TestAPolicyWithNoApprovalRuleIsUnchanged(t *testing.T) {
	withFloors(t, &grants.Floors{Floors: map[string][]grants.FloorRule{
		"widget.build": {{Kind: "allowlist", Param: "cluster", Values: []string{"a"}}},
	}})
	if required, _, _ := approvalGate("widget.build", "", "dev-alice",
		map[string]any{"env": "prod"}); required {
		t.Fatal("an unrelated policy started requiring approval")
	}
}

// Never silent: a rule with no message still tells the requester what to do.
func TestAGateWithNoMessageStillSaysSomething(t *testing.T) {
	withFloors(t, &grants.Floors{Floors: map[string][]grants.FloorRule{
		"widget.build": {{Kind: "require_approval_when", WhenParam: "env", WhenValue: "prod"}},
	}})
	_, reason, _ := approvalGate("widget.build", "", "dev-alice", map[string]any{"env": "prod"})
	if reason == "" {
		t.Fatal("a refusal with no words sends the requester to find the path that does not ask")
	}
}
