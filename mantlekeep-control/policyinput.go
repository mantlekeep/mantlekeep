package mantlekeep

// PolicyInputFor builds the evaluator's input from a RESOLVED subject and the intent it is
// acting on.
//
// It exists so there is exactly ONE construction of a policy input in the system. The door
// builds one to decide; anything that asks "what WOULD the door decide" must build the same
// one, and a second copy of these three lines is a copy that drifts. When it drifts, the thing
// that reports a decision reports a decision the door would not make — which is worse than
// reporting nothing, because a permission answer is believed.
//
// The subject is the resolved one, never the claimed one. Roles are the resolver's to fill: a
// caller that could assert its own roles could assert its way past any gate, and that stays
// true of an explanation as much as of a decision.
//
// Requester, env and scope are read GENERICALLY off params. The engine names no verb and no
// environment — an env-gated action is a grant plus a required_role_when rule in the floor
// DATA — so these three are lifted out only because the typed input has fields for them, not
// because the engine knows what they mean.
func PolicyInputFor(subject Subject, intent Intent) PolicyInput {
	requester, _ := intent.Params["requester"].(string)
	env, _ := intent.Params["env"].(string)
	scope, _ := intent.Params["scope"].(string)

	return PolicyInput{
		Subject: PolicySubject{
			ID:    subject.ID,
			Roles: subject.Roles,
			IsAI:  subject.IsAI,
			Attrs: subject.Attrs,
		},
		Intent: PolicyIntent{
			Action:    intent.Action,
			Resource:  intent.Resource,
			Requester: requester,
			Env:       env,
			Goal:      intent.Spec.Goal,
			Scope:     scope,
			Params:    intent.Params,
		},
	}
}

// EvaluationStep is ONE stage of the engine's decision, in the order it is asked.
//
// The order is the interesting part of a policy engine and the part a person cannot read off a
// grant table. It is what makes "config may tighten, never loosen" structural rather than
// conventional: every deny is asked BEFORE the approval gate, so no policy document can reach
// past a closed decision and re-open it as merely awaiting a signature.
//
// It is described here, beside the ports, so a surface that explains a refusal describes the
// engine's real order rather than a plausible one. The engine that implements it publishes the
// list; a behavioural test drives each step and fails if the code stops matching the words.
type EvaluationStep struct {
	// Name identifies the stage.
	Name string `json:"name"`
	// Outcome is what this stage can produce: a deny, or the approval gate's require_approval.
	Outcome DecisionAction `json:"outcome"`
	// Source says whether a deployment can change this stage. "engine" is a floor — it is code,
	// not data, and no configuration reaches it. "policy" is a document somebody can edit.
	// A reader who cannot tell the two apart cannot tell a rule they may argue with from a rule
	// they may not.
	Source EvaluationSource `json:"source"`
	// Detail is the one sentence a person needs to know what this stage asked.
	Detail string `json:"detail"`
}

// EvaluationSource says whether a stage is code or configuration.
type EvaluationSource string

const (
	// SourceEngine is a stage compiled into the engine. Configuration cannot reach it.
	SourceEngine EvaluationSource = "engine"
	// SourcePolicy is a stage driven by a policy document a deployment can change.
	SourcePolicy EvaluationSource = "policy"
)

// The stage names the default engine publishes and stamps onto every decision it makes.
//
// Constants rather than literals because they are used in three places that must agree: the
// published order, the [Decision.Step] each stage stamps, and any surface that locates a
// refusal in the order. Two of those spelling it differently is a page pointing at the wrong
// rule, which is the failure this whole mechanism exists to prevent.
const (
	StepGoalStated         = "goal is stated"
	StepAINotApproving     = "an AI is not performing an approval"
	StepRolePermits        = "a role permits the action"
	StepSeparationOfDuties = "the approver is not the requester"
	StepProductAdmits      = "the owning product admits the request"
	StepAttributeFloor     = "the attribute floor admits the request"
	StepApprovalGate       = "does a second person have to sign off"
)
