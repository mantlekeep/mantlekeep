package policy

import mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"

// EvaluationOrder publishes the order [RBAC.Evaluate] asks its questions in.
//
// It lives beside the evaluator, not in the surface that renders it, because a description of
// an order kept in another module is a description that drifts silently — and the drift is
// invisible exactly when it matters, since a stale explanation still reads like an answer.
// order_test.go DRIVES each step against the real engine, so the words below fail the build
// when the code stops matching them.
//
// Read it top to bottom: every deny is asked before the approval gate. That is what makes
// "config may TIGHTEN, never LOOSEN" a property of the CALL ORDER rather than of a rule
// ordering a human edits — no document can reach past a closed decision and re-open it as
// merely awaiting a signature.
func EvaluationOrder() []mantlekeep.EvaluationStep {
	return []mantlekeep.EvaluationStep{
		{
			Name: mantlekeep.StepGoalStated, Outcome: mantlekeep.ActionDeny, Source: mantlekeep.SourceEngine,
			Detail: "An intent with no goal is refused before anything else is asked. " +
				"Declare-before-execute is not optional, so it is not configurable.",
		},
		{
			Name: mantlekeep.StepAINotApproving, Outcome: mantlekeep.ActionDeny,
			Source: mantlekeep.SourceEngine,
			Detail: "An AI subject may never perform an approval-shaped action, whatever role it " +
				"holds. WHICH actions are approval-shaped is policy data (approval_actions); " +
				"that an AI can never perform them is the engine, and no grant reaches it.",
		},
		{
			Name: mantlekeep.StepRolePermits, Outcome: mantlekeep.ActionDeny, Source: mantlekeep.SourcePolicy,
			Detail: "The subject's roles must grant the action — from the grant document, from a " +
				"registered product adapter, or from the layered config cascade's action→role " +
				"binding. This is the line the grant table below shows.",
		},
		{
			Name: mantlekeep.StepSeparationOfDuties, Outcome: mantlekeep.ActionDeny,
			Source: mantlekeep.SourceEngine,
			Detail: "When the intent names a requester, the acting subject must not be that same " +
				"person. Nobody approves their own request; there is no configuration that " +
				"permits it.",
		},
		{
			Name: mantlekeep.StepProductAdmits, Outcome: mantlekeep.ActionDeny,
			Source: mantlekeep.SourcePolicy,
			Detail: "If a registered product adapter owns this action, its own attribute floor " +
				"rules on the concrete parameters. An action no adapter owns skips this.",
		},
		{
			Name: mantlekeep.StepAttributeFloor, Outcome: mantlekeep.ActionDeny,
			Source: mantlekeep.SourcePolicy,
			Detail: "The floor rules for this action are applied to its parameters. Layers may " +
				"only ADD rules — platform first, then each product — so a lower layer can " +
				"tighten the floor and can never remove a rule from it.",
		},
		{
			Name: mantlekeep.StepApprovalGate, Outcome: mantlekeep.ActionRequireApproval,
			Source: mantlekeep.SourcePolicy,
			Detail: "Asked LAST, once nothing above has refused. A require_approval_when rule " +
				"turns an allow into 'a person must sign off'. It is the only rule that does not " +
				"deny, and because it is asked after every deny it can never re-open one.",
		},
	}
}
