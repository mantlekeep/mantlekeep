package app

import (
	"context"
	"fmt"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Explainer answers "what would the door decide" — and decides nothing.
//
// # Why this is not the door
//
// The door DECIDES: it resolves identity, evaluates, records the decision on the hash chain,
// and issues an execution token. Every one of those is a consequence, and a person asking why
// they were refused must not cause any of them. An explanation that recorded a decision would
// bury the real changes in a chain of page loads; one that issued a token would hand out the
// thing the refusal was withholding.
//
// # Why it is not a second engine either
//
// It resolves the subject through the SAME resolver and evaluates through the SAME evaluator
// the door holds, over an input built by the SAME [mantlekeep.PolicyInputFor]. There is no second
// implementation of anything here — which is the only way an explanation can be trusted. A
// screen that re-implemented the rules would disagree with the door eventually, and the day it
// did, the person would be reading the wrong one.
//
// # What it discloses
//
// It answers about any subject, not only the caller — because "why was Alice refused" is the
// question a lead actually has, and a tool that can only answer about yourself cannot be used
// to fix anybody else's problem. It therefore reveals which roles the directory gives a named
// person. A deployment that considers that sensitive gates the surface in front of it; nothing
// here should be exposed unauthenticated.
type Explainer struct {
	identity mantlekeep.IdentityResolver
	policy   mantlekeep.PolicyEvaluator
}

// NewExplainer pairs the door's identity resolver with the door's evaluator.
//
// Both are required. An explainer built over a different evaluator than the door uses would be
// a confident second opinion, and its wrongness would be invisible.
func NewExplainer(identity mantlekeep.IdentityResolver, evaluator mantlekeep.PolicyEvaluator) *Explainer {
	return &Explainer{identity: identity, policy: evaluator}
}

// Explain resolves the subject and evaluates the intent, returning both.
//
// The resolved subject travels back because it is half the answer: a refusal is usually not
// "this action is forbidden" but "the directory does not give you the role that holds it", and
// a person shown only the verdict cannot tell those apart.
//
// An unresolvable subject is an ERROR rather than a deny. The door does not know that person,
// which is a different fact from a policy refusing them, and reporting it as a refusal sends
// somebody to argue about a grant when the answer is that their identity never arrived.
func (e *Explainer) Explain(ctx context.Context, intent mantlekeep.Intent) (mantlekeep.Subject, mantlekeep.Decision, error) {
	if e.identity == nil || e.policy == nil {
		return mantlekeep.Subject{}, mantlekeep.Decision{}, fmt.Errorf(
			"explain: this deployment wired no identity resolver or no policy engine, so nothing " +
				"here can say what the door would decide")
	}

	subject, err := e.identity.Resolve(ctx, mantlekeep.ExternalIdentity{
		ID:     intent.Subject.ID,
		Groups: intent.Subject.ADGroups,
	})
	if err != nil {
		return mantlekeep.Subject{}, mantlekeep.Decision{}, fmt.Errorf(
			"the door does not know %q: %w", intent.Subject.ID, err)
	}

	decision, err := e.policy.Evaluate(ctx, mantlekeep.PolicyInputFor(subject, intent))
	if err != nil {
		return subject, mantlekeep.Decision{}, fmt.Errorf("policy error: %w", err)
	}
	return subject, decision, nil
}
