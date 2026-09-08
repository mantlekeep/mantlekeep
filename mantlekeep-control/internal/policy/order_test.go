package policy

import (
	"context"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

const litOrderClusterIs = "cluster is not on the approved list"

// EvaluationOrder is WORDS about code, and words about code go stale. These tests drive the
// real engine once per described step, so a stage that is renamed, reordered, or quietly
// stopped denying fails the build here rather than being explained wrongly to a person on a
// permission screen.

// grantsAction is the minimal ActionAuthorizer: it binds ONE action to ONE required role, the
// same seam a product uses (RBAC.WithDynamic). Used here so a test can grant an action without
// editing the shared grant document.
type grantsAction struct {
	action string
	role   mantlekeep.Role
}

func (g grantsAction) RequiredRole(action string) (mantlekeep.Role, bool) {
	if action == g.action {
		return g.role, true
	}
	return "", false
}

// withFloorRules installs floor rules for one action for the duration of a test.
//
// It swaps a whole replacement snapshot rather than writing into the live floor map, because the
// live map is the law the door is deciding against and a test must not edit it in place.
func withFloorRules(t *testing.T, action string, rules ...grants.FloorRule) {
	t.Helper()
	previous := ensurePolicy()

	seeded := &grants.Floors{Floors: make(map[string][]grants.FloorRule, len(previous.floors.Floors)+1)}
	for existing, existingRules := range previous.floors.Floors {
		seeded.Floors[existing] = existingRules
	}
	seeded.Floors[action] = rules

	replacement := *previous
	replacement.floors = seeded
	livePolicy.Store(&replacement)
	t.Cleanup(func() { livePolicy.Store(previous) })
}

// withApprovalAction declares one action approval-shaped for the duration of a test.
//
// Declared here rather than relied on from an ambient merged policy: a test that depends on a
// product document happening to list an action passes for a reason that is not in the test, and
// stops passing when that document changes for unrelated reasons.
func withApprovalAction(t *testing.T, action string) {
	t.Helper()
	previous := ensurePolicy()

	seeded := make(map[string]bool, len(previous.approvalActions)+1)
	for existing := range previous.approvalActions {
		seeded[existing] = true
	}
	seeded[action] = true

	replacement := *previous
	replacement.approvalActions = seeded
	livePolicy.Store(&replacement)
	t.Cleanup(func() { livePolicy.Store(previous) })
}

// orderAction is a name no product doc uses, so these tests neither disturb nor are disturbed
// by the merged real policy TestMain loads.
const orderAction = "test.order"

// grantOrderAction makes orderAction issuable by an L3-Consumer, through the same
// ActionAuthorizer seam a Canvas-authored product uses.
func orderEngine() *RBAC {
	return NewRBAC().WithDynamic(grantsAction{orderAction, mantlekeep.RoleConsumer})
}

func evaluateOrder(t *testing.T, engine *RBAC, input mantlekeep.PolicyInput) mantlekeep.Decision {
	t.Helper()
	decision, err := engine.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return decision
}

// Every published step must have a case here that PRODUCES its declared outcome. Driving the
// list itself is what makes the guard survive a new step: adding one to EvaluationOrder with
// no proof fails immediately, instead of shipping a sentence nobody checked.
func TestEveryPublishedStepProducesItsDeclaredOutcome(t *testing.T) {
	person := mantlekeep.PolicySubject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}}
	robot := mantlekeep.PolicySubject{ID: "ai-bot", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true}

	// One input per step, each tripping only the step it proves.
	cases := map[string]func(t *testing.T) mantlekeep.Decision{
		"goal is stated": func(t *testing.T) mantlekeep.Decision {
			return evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
				Subject: person, Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: ""}})
		},
		"an AI is not performing an approval": func(t *testing.T) mantlekeep.Decision {
			const approvalShaped = "test.order.approve"
			withApprovalAction(t, approvalShaped)
			return evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
				Subject: robot, Intent: mantlekeep.PolicyIntent{Action: approvalShaped, Goal: "sign off"}})
		},
		"a role permits the action": func(t *testing.T) mantlekeep.Decision {
			// No dynamic grant: nobody may issue this action at all.
			return evaluateOrder(t, NewRBAC(), mantlekeep.PolicyInput{
				Subject: person, Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g"}})
		},
		"the approver is not the requester": func(t *testing.T) mantlekeep.Decision {
			return evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
				Subject: person,
				Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g",
					Requester: person.ID}})
		},
		"the owning product admits the request": func(t *testing.T) mantlekeep.Decision {
			engine := orderEngine().WithProviders(refusingProvider{})
			return evaluateOrder(t, engine, mantlekeep.PolicyInput{
				Subject: person, Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g"}})
		},
		"the attribute floor admits the request": func(t *testing.T) mantlekeep.Decision {
			withFloorRules(t, orderAction, grants.FloorRule{
				Kind: "allowlist", Param: "cluster", Values: []string{"approved-1"},
				Message: litOrderClusterIs})
			return evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
				Subject: person,
				Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g",
					Params: map[string]any{"cluster": "somebody-elses"}}})
		},
		"does a second person have to sign off": func(t *testing.T) mantlekeep.Decision {
			withFloorRules(t, orderAction, grants.FloorRule{
				Kind: "require_approval_when", WhenParam: "gate", WhenValue: "owning-team",
				Role: "L2-Operator", Message: "a second person must sign off"})
			return evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
				Subject: person,
				Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g",
					Params: map[string]any{"gate": "owning-team"}}})
		},
	}

	for _, step := range EvaluationOrder() {
		drive, described := cases[step.Name]
		if !described {
			t.Fatalf("EvaluationOrder publishes the step %q with no case that proves it — a "+
				"permission screen would explain a stage nobody drove", step.Name)
		}
		t.Run(step.Name, func(t *testing.T) {
			got := drive(t)
			if got.Action != step.Outcome {
				t.Fatalf("step %q is published as producing %q but the engine answered %q (%q)",
					step.Name, step.Outcome, got.Action, got.Reason)
			}
			// The engine STAMPS the stage onto the decision. If it stamps a different one than
			// it publishes, a page locating a refusal in the order points at the wrong rule —
			// silently, and confidently.
			if got.Step != step.Name {
				t.Fatalf("the engine stamped step %q on a decision published as coming from %q "+
					"(%q) — a surface would point a person at the wrong rule",
					got.Step, step.Name, got.Reason)
			}
		})
	}
}

// The claim the whole page rests on: the approval gate is asked LAST. Each of these trips a
// deny AND the gate at once, and the deny must win — otherwise a policy document could bury a
// refusal under a signature request, and "tighten, never loosen" would be a convention rather
// than a property of the call order.
func TestEveryDenyIsAskedBeforeTheApprovalGate(t *testing.T) {
	gate := grants.FloorRule{
		Kind: "require_approval_when", WhenParam: "gate", WhenValue: "owning-team",
		Role: "L2-Operator", Message: "a second person must sign off",
	}
	person := mantlekeep.PolicySubject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}}
	gated := map[string]any{"gate": "owning-team"}

	t.Run("no goal", func(t *testing.T) {
		withFloorRules(t, orderAction, gate)
		decision := evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
			Subject: person,
			Intent:  mantlekeep.PolicyIntent{Action: orderAction, Goal: "", Params: gated}})
		mustDeny(t, decision, "an intent with no goal became one merely awaiting a signature")
	})

	t.Run("the owning product refuses", func(t *testing.T) {
		withFloorRules(t, orderAction, gate)
		engine := orderEngine().WithProviders(refusingProvider{})
		decision := evaluateOrder(t, engine, mantlekeep.PolicyInput{
			Subject: person,
			Intent:  mantlekeep.PolicyIntent{Action: orderAction, Goal: "g", Params: gated}})
		mustDeny(t, decision, "a product's own refusal became a signature request")
	})

	t.Run("the attribute floor refuses", func(t *testing.T) {
		withFloorRules(t, orderAction, gate, grants.FloorRule{
			Kind: "allowlist", Param: "cluster", Values: []string{"approved-1"},
			Message: litOrderClusterIs})
		decision := evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
			Subject: person,
			Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g",
				Params: map[string]any{"gate": "owning-team", "cluster": "somebody-elses"}}})
		mustDeny(t, decision, "a floor deny was re-opened as a change awaiting approval")
	})
}

// The grant check is asked before the floors, so a person with no grant is told they have no
// grant rather than being sent to fix a parameter that was never the reason.
func TestTheMissingGrantIsReportedBeforeAnyFloorDetail(t *testing.T) {
	withFloorRules(t, orderAction, grants.FloorRule{
		Kind: "allowlist", Param: "cluster", Values: []string{"approved-1"},
		Message: litOrderClusterIs})

	decision := evaluateOrder(t, NewRBAC(), mantlekeep.PolicyInput{
		Subject: mantlekeep.PolicySubject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
		Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g",
			Params: map[string]any{"cluster": "somebody-elses"}}})

	mustDeny(t, decision, "an ungranted action was not denied")
	if decision.Reason != "no role permits action "+orderAction {
		t.Fatalf("reason = %q, want the missing grant — a person sent to fix a cluster name "+
			"will fix it and be refused again", decision.Reason)
	}
}

// InForce must report what the ENGINE holds, including the wildcard the engine adds itself.
// A screen that showed only the documents would understate the most powerful role there is.
func TestInForceReportsTheEnginesOwnWildcard(t *testing.T) {
	roles, floors := InForce()
	if floors == nil {
		t.Fatal("InForce returned no floor document")
	}
	held := roles.RoleActions[string(mantlekeep.RoleSuperAdmin)]
	found := false
	for _, action := range held {
		if action == "*" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s is reported as holding %v — the engine's wildcard is missing, so the one "+
			"screen that shows who may do what understates the role that may do everything",
			mantlekeep.RoleSuperAdmin, held)
	}
}

// The documents handed out must be a COPY, all the way down. A rendering bug that edited them
// would be editing the law the door enforces — and the aliasing that matters is the one INSIDE
// the maps, since a fresh outer map with the same slices in it is not a copy.
func TestInForceHandsOutACopyOfTheLaw(t *testing.T) {
	withFloorRules(t, orderAction, grants.FloorRule{
		Kind: "allowlist", Param: "cluster", Values: []string{"approved-1"},
		Message: litOrderClusterIs})

	roles, floors := InForce()
	// Edit INSIDE the returned documents: a rule's message, a rule's values, a role's actions.
	floors.Floors[orderAction][0].Message = "tampered"
	floors.Floors[orderAction][0].Values[0] = "tampered"
	roles.RoleActions["L0-SuperAdmin"] = append(roles.RoleActions["L0-SuperAdmin"], "tampered")

	again, alsoFloors := InForce()
	if alsoFloors.Floors[orderAction][0].Message == "tampered" {
		t.Error("editing a returned rule's message changed the floor the door enforces")
	}
	if alsoFloors.Floors[orderAction][0].Values[0] == "tampered" {
		t.Error("editing a returned rule's values changed the floor the door enforces")
	}
	for _, action := range again.RoleActions["L0-SuperAdmin"] {
		if action == "tampered" {
			t.Error("editing the returned grants changed the policy in force")
		}
	}
	// And the engine must still decide as it did: the real proof that nothing moved.
	decision := evaluateOrder(t, orderEngine(), mantlekeep.PolicyInput{
		Subject: mantlekeep.PolicySubject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
		Intent: mantlekeep.PolicyIntent{Action: orderAction, Goal: "g",
			Params: map[string]any{"cluster": "approved-1"}}})
	if decision.Action != mantlekeep.ActionAllow {
		t.Fatalf("after a caller edited the documents it was handed, the engine answered %q "+
			"(%q) — the page was holding the law itself", decision.Action, decision.Reason)
	}
}

// refusingProvider owns orderAction and always refuses it — the product-adapter stage.
type refusingProvider struct{}

func (refusingProvider) Name() string                              { return "test-provider" }
func (refusingProvider) Actions() []string                         { return []string{orderAction} }
func (refusingProvider) RoleActions() map[mantlekeep.Role][]string { return nil }
func (refusingProvider) Admit(mantlekeep.PolicyIntent, []mantlekeep.Role) (bool, string) {
	return true, "the owning product refuses this request"
}

func mustDeny(t *testing.T, decision mantlekeep.Decision, complaint string) {
	t.Helper()
	if decision.Action != mantlekeep.ActionDeny {
		t.Fatalf("decision = %q (%q) — %s", decision.Action, decision.Reason, complaint)
	}
}
