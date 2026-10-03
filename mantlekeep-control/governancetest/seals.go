package governancetest

// The seals, driven through the real door: the approval gate, the separation-of-duties refusal,
// and the AI guardrail.
//
// These are behavioural on purpose. Each of them is a claim a deployment makes in a compliance
// answer, and a claim that has never been fired is a claim about code, not about this
// deployment. The documents check says the rule is written; this says the door applies it.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// noRequester blanks the requester param for a probe that must look like an ORIGINAL request.
//
// The engine's approval gate skips any intent naming a requester other than the acting subject
// — that is how an approver's re-submission completes rather than opening another approval
// forever. An action whose representative params happen to carry a requester would therefore
// sail through the gate and look ungated, so the probes state which one they mean.
var noRequester = map[string]any{"requester": ""}

// aGatedActionWaitsForASecondPerson fires the gate the documents check only reads about.
//
// The failure this catches is the fail-open one: the door ALLOWS a change the deployment
// declared gated, issues a token, and writes an allow to the hash chain. Nothing afterwards can
// tell that record apart from one a person signed.
func aGatedActionWaitsForASecondPerson(t *testing.T, deployment Deployment) {
	var allowed, deniedElsewhere []string
	gated := 0
	for _, action := range deployment.Actions {
		if !action.Gated {
			continue
		}
		gated++
		for _, subject := range deployment.humanSubjects() {
			outcome := deployment.probe(t, subject, action, noRequester)
			switch {
			case outcome.awaitingAPerson():
				return // the gate held for a real subject on a real action
			case outcome.allowed:
				allowed = append(allowed, fmt.Sprintf("%s→%s", subject.ID, action.Name))
			case outcome.refusedForNoGrant():
				// This subject cannot reach the action; another may.
			default:
				deniedElsewhere = append(deniedElsewhere,
					fmt.Sprintf("%s→%s: %s", subject.ID, action.Name, outcome.describe()))
			}
		}
	}
	if gated == 0 {
		t.Skip("no declared action asserts Gated, so there is no gate to fire — see the " +
			"gated-action document check")
	}
	if len(allowed) > 0 {
		sort.Strings(allowed)
		t.Fatalf("the door ALLOWED an action this deployment declared gated (%s): it issued an "+
			"execution token and wrote an allow to the hash chain, so the change proceeded on "+
			"one person's say-so and the audit record cannot be told apart from one a second "+
			"person signed. This fails OPEN. Add a %q rule for the action in floors in %s, "+
			"reading the param its intents carry",
			strings.Join(allowed, ", "), requireApprovalWhen, floorsDocument)
	}
	if len(deniedElsewhere) > 0 {
		sort.Strings(deniedElsewhere)
		t.Fatalf("no gated action reached the approval gate — every attempt was refused earlier "+
			"(%s). The gate is asked LAST, after every deny rule, so these params can never "+
			"prove it fires. Supply Params that clear the deny rules for the action, or the "+
			"gate stays a rule nobody has ever seen run", strings.Join(deniedElsewhere, "; "))
	}
	t.Fatal("no supplied subject is granted any gated action, so the approval gate was never " +
		"reached — see the reachability check, which names the same cause")
}

// nobodyApprovesTheirOwnChange submits a change and then tries to approve it as the same person.
//
// The refusal must be a SEPARATION-OF-DUTIES one, not merely any refusal: a deny that happens to
// arrive from a missing grant or an attribute cap would let this pass on a deployment where the
// separation-of-duties clause does nothing, which is precisely the state worth finding.
func nobodyApprovesTheirOwnChange(t *testing.T, deployment Deployment) {
	var reached []string
	for _, action := range deployment.gatedActionsFirst() {
		for _, subject := range deployment.humanSubjects() {
			// The change itself. Its outcome does not matter — allowed or awaiting a person,
			// the subject has now asked for something.
			opened := deployment.probe(t, subject, action, noRequester)
			if opened.refusedForNoGrant() {
				continue // this pair cannot reach the clause; try another
			}
			// The approval of that same change, by the same person. requester names who asked;
			// the acting subject is the same id, which is the whole offence.
			approval := deployment.probe(t, subject, action, map[string]any{"requester": subject.ID})
			if approval.decision.Category == mantlekeep.DenialSeparationOfDuties {
				return
			}
			if approval.allowed {
				t.Fatalf("%q approved its own change to %q: the door issued an execution token "+
					"for an intent naming that same id as the requester. Separation of duties "+
					"is not being enforced for this action, and the chain records a "+
					"second-person approval that never happened. The engine's clause is "+
					"unconditional, so check that the product really sends the original "+
					"requester in params[\"requester\"] — a gate nobody passes the requester to "+
					"cannot tell one person from two", subject.ID, action.Name)
			}
			reached = append(reached, fmt.Sprintf("%s→%s: %s", subject.ID, action.Name, approval.describe()))
		}
	}
	if len(reached) == 0 {
		t.Fatal("no supplied subject is granted any declared action, so the " +
			"separation-of-duties refusal was never reached — see the reachability check, " +
			"which names the same cause")
	}
	sort.Strings(reached)
	t.Fatalf("a self-approval was refused, but never for separation of duties (%s). The refusal "+
		"an operator gets sends them somewhere else entirely, and if the earlier rule is ever "+
		"relaxed the self-approval becomes an allow with nothing behind it. Expected category "+
		"%q at %q", strings.Join(reached, "; "), mantlekeep.DenialSeparationOfDuties,
		mantlekeep.StepSeparationOfDuties)
}

// anAIIsNeverTheApprover fires the sealed floor: an AI may never perform an approval action,
// whatever role it holds.
//
// It is the one check whose input the deployment cannot get wrong quietly. The seal matches
// action names against approval_actions, so on a deployment that left that list empty there is
// nothing to fire and the theAISealHasAmmunition check already says so — this one would pass
// vacuously, which is why it skips instead.
func anAIIsNeverTheApprover(t *testing.T, deployment Deployment, held *grants.Grants) {
	agents := deployment.aiSubjects()
	if len(agents) == 0 {
		t.Skip("no Subject is marked IsAI, so the sealed floor was not fired. Add the " +
			"deployment's AI service-account id to Subjects: an untested seal is a claim about " +
			"the engine, not about this deployment")
	}
	if len(held.ApprovalActions) == 0 {
		t.Skipf("approval_actions in %s is empty, so the seal has no action to match and this "+
			"check would pass without firing anything — see the approval_actions check",
			grantsDocument)
	}
	for _, agent := range agents {
		for _, approvalAction := range held.ApprovalActions {
			action := deployment.declaredAction(approvalAction)
			outcome := deployment.probe(t, agent, action, map[string]any{"requester": ""})
			if outcome.decision.Step == mantlekeep.StepAINotApproving {
				continue // sealed, as it must be
			}
			t.Errorf("AI subject %q was not stopped by the sealed floor on approval action %q: "+
				"the door answered %s. The seal fires on approval_actions in %s BEFORE any role "+
				"check, so either that list does not name this action or the door's directory "+
				"does not flag %q as an AI — and the door reads IsAI from its own directory, "+
				"never from what a caller passed here. Until it fires, an AI service account "+
				"can be the second person on its own work",
				agent.ID, approvalAction, outcome.describe(), grantsDocument, agent.ID)
		}
	}
}

// declaredAction returns the deployment's declaration of an action by name, so a probe carries
// the params that action's intents really hold. An approval action the deployment did not
// declare is probed with no params at all: the AI seal reads only the action name, so it fires
// either way, and inventing params would be the suite asserting a shape it was not told.
func (deployment Deployment) declaredAction(name string) Action {
	for _, action := range deployment.Actions {
		if action.Name == name {
			return action
		}
	}
	return Action{Name: name}
}

func (deployment Deployment) aiSubjects() []mantlekeep.Subject {
	var agents []mantlekeep.Subject
	for _, subject := range deployment.Subjects {
		if subject.IsAI {
			agents = append(agents, subject)
		}
	}
	return agents
}
