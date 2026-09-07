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
