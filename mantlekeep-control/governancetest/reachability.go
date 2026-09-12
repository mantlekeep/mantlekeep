package governancetest

// Reachability: does anybody this deployment knows about get past the role step at all?
//
// This is the engine's own answer to the question noFloorRuleIsUnreachable asks of the
// documents, and it is the one that sees a grant arriving from a registered policy provider or
// a config layer — grants that are real, that the engine honours, and that appear in no
// document this suite can read.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// someSubjectCanReachSomeAction proves that at least one supplied subject can issue at least
// one declared action.
//
// When nothing is reachable, every check further down is testing an unreachable code path: a
// gate never asked, a floor never evaluated, a separation-of-duties clause the request never
// gets as far as. That deployment denies everything, which is the safe failure — but a suite
// that reported the seals below as GREEN would be describing controls that have never run.
func someSubjectCanReachSomeAction(t *testing.T, deployment Deployment) {
	var refusals []string
	for _, subject := range deployment.Subjects {
		for _, action := range deployment.Actions {
			outcome := deployment.probe(t, subject, action, map[string]any{"requester": ""})
			if !outcome.refusedForNoGrant() {
				return // somebody got past the role step; that is all this check needs
			}
			refusals = append(refusals, fmt.Sprintf("%s→%s", subject.ID, action.Name))
		}
	}
	sort.Strings(refusals)
	t.Fatalf("every subject was refused every declared action at \"a role permits the action\" "+
		"(%s). Nothing in this deployment is reachable, so every floor rule, approval gate and "+
		"seal below is code that has never been asked. Grant the product's actions to the roles "+
		"that may issue them in role_actions in %s — and check that the subject ids resolve to "+
		"the roles you granted, because the door reads roles from its own directory and ignores "+
		"whatever a caller claims", strings.Join(refusals, ", "), grantsDocument)
}
