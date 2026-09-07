package policy

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// TestEvaluateStep proves per-step role + separation-of-duties on the canvas:
// "create the job, then someone ELSE with the right role must trigger it."
func TestEvaluateStep(t *testing.T) {
	r := NewRBAC()
	sub := func(id string, role mantlekeep.Role, ai bool) mantlekeep.PolicySubject {
		return mantlekeep.PolicySubject{ID: id, Roles: []mantlekeep.Role{role}, IsAI: ai}
	}

	cases := []struct {
		name  string
		in    StepAuth
		allow bool
	}{
		{
			name:  "author cannot trigger own approval-gated step (SoD blocks)",
			in:    StepAuth{Subject: sub("alice", mantlekeep.RoleOperator, false), Step: "deploy-prod", Author: "alice", RequireApproval: true},
			allow: false,
		},
		{
			name:  "a DIFFERENT person with the role may trigger it",
			in:    StepAuth{Subject: sub("bob", mantlekeep.RoleOperator, false), Step: "deploy-prod", Author: "alice", RequireApproval: true},
			allow: true,
		},
		{
			name:  "under-role person is blocked by the per-step floor",
			in:    StepAuth{Subject: sub("carol", mantlekeep.RoleConsumer, false), Step: "deploy-prod", RunAs: mantlekeep.RoleArchitect, Author: "alice"},
			allow: false,
		},
		{
			name:  "high-enough role passes the per-step floor",
			in:    StepAuth{Subject: sub("dan", mantlekeep.RoleArchitect, false), Step: "deploy-prod", RunAs: mantlekeep.RoleArchitect, Author: "alice"},
			allow: true,
		},
		{
			name:  "AI agent can never satisfy separation of duties",
			in:    StepAuth{Subject: sub("ai-1", mantlekeep.RoleConsumer, true), Step: "deploy-prod", Author: "alice", RequireApproval: true},
			allow: false,
		},
		{
			name:  "plain step with no floor and no approval is allowed",
			in:    StepAuth{Subject: sub("eve", mantlekeep.RoleConsumer, false), Step: "checkout", Author: "eve"},
			allow: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := r.EvaluateStep(c.in)
			allowed := got.Action == mantlekeep.ActionAllow
			if allowed != c.allow {
				t.Fatalf("allow=%v want %v (reason: %s)", allowed, c.allow, got.Reason)
			}
		})
	}
}
