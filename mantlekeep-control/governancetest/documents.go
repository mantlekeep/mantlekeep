package governancetest

// The DOCUMENT checks: everything that can be decided by reading the policy documents the
// engine loaded, without submitting anything. They are here rather than folded into the
// behavioural probes because a document defect has an exact address — a key in a named file —
// and an operator who is given that address does not have to work backwards from a refusal.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// requireApprovalWhen is the one floor rule kind that does not deny: it turns an allow into
// "a person must sign this". A gated action with no rule of this kind is gated by nobody.
const requireApprovalWhen = "require_approval_when"

// someRoleIsGrantedSomeAction is the loud, fail-closed case, checked first because every other
// finding is downstream of it: with no grants, every action is refused at the role step, so no
// floor, gate or seal below it is ever asked.
func someRoleIsGrantedSomeAction(t *testing.T, held *grants.Grants) {
	if len(grantedActions(held)) == 0 {
		t.Fatalf("role_actions in %s grants nothing (%d role(s) listed, 0 actions between "+
			"them). MantleKeep ships this document EMPTY on purpose, so an unconfigured "+
			"deployment denies every action at \"a role permits the action\" and only the "+
			"engine's built-in L0-SuperAdmin wildcard still works. Add the product's actions "+
			"under the roles that may issue them", grantsDocument, len(held.RoleActions))
	}
}

// theAISealHasAmmunition checks the one guardrail that is real code with an empty input.
//
// internal/policy/rbac.go refuses `isAI && approvalActions()[action]` before it asks anything
// else, which is exactly right and completely inert while approval_actions is []. The
// deployment then has the seal in its binary, in its architecture diagram, and in its
// compliance answer, and an AI service account holding a granted role can perform every
// approval the product has.
func theAISealHasAmmunition(t *testing.T, held *grants.Grants) {
	granted := grantedActions(held)
	if len(granted) == 0 {
		t.Skip("nothing is granted yet, so the seal has nothing to guard — see the " +
			"role_actions check, which names the same document")
	}
	if len(held.ApprovalActions) == 0 {
		t.Fatalf("approval_actions in %s is empty while role_actions grants %d action(s) (%s). "+
			"The \"an AI may never approve\" seal is real code matching against an empty set, "+
			"so it can never fire: an AI subject holding a granted role may perform any of "+
			"those actions, including the ones a person is supposed to sign. List the "+
			"approval-shaped actions there", grantsDocument, len(granted), summarise(granted))
	}
}

// everyGatedActionHasARuleThatFires is THE check. It is the only failure mode here that fails
// OPEN, and the only one where the audit record actively misleads.
//
// A product computes a gate param, puts it on the intent, and the door writes the decision to
// the hash chain. If no require_approval_when rule reads that param, the door returns an allow,
// the chain records an allow, and every surface downstream reports a governed change — one that
// no person ever signed. Nobody is looking for a missing rule, because everything appears to
// work; the gate is visible in the request and in the record, and absent only from the law.
//
// A rule that EXISTS but never matches these params is reported separately, because the two send
// an operator to different edits: one is a rule to write, the other is a whenValue to correct.
func everyGatedActionHasARuleThatFires(t *testing.T, deployment Deployment, floors *grants.Floors) {
	gatedCount := 0
	for _, action := range deployment.Actions {
		if !action.Gated {
			continue
		}
		gatedCount++
		reportGateFor(t, action, floors.Floors[action.Name])
	}
	if gatedCount == 0 {
		t.Skip("no declared action asserts Gated — set Gated on the actions that must not " +
			"proceed on one person's say-so, or this suite cannot tell an ungated deployment " +
			"from an unenforced gate")
	}
}

// reportGateFor judges ONE gated action against the rules the floors document holds for it.
func reportGateFor(t *testing.T, action Action, rules []grants.FloorRule) {
	approvalRules := rulesOfKind(rules, requireApprovalWhen)
	if len(approvalRules) == 0 {
		t.Errorf("action %q is declared GATED but floors[%q] in %s holds no %q rule (%d rule(s) "+
			"of other kinds). Its gate param is computed, carried on the intent and written to "+
			"the hash chain, and nothing reads it: the door allows the change on one person's "+
			"say-so and the audit record says it was governed. This fails OPEN — add a %q rule "+
			"naming the param that carries the gate (this intent carries %s)",
			action.Name, action.Name, floorsDocument, requireApprovalWhen, len(rules),
			requireApprovalWhen, summariseParams(action.Params))
		return
	}
	for _, rule := range approvalRules {
		if whenMatches(action.Params, rule.WhenParam, rule.WhenValue) {
			return // a rule that really fires for a really-shaped intent
		}
	}
	t.Errorf("action %q is declared GATED and floors[%q] in %s holds %d %q rule(s), but none of "+
		"them MATCHES the params its intents carry: the rules read %s, the intent carries %s. "+
		"A rule that never matches is the same outcome as no rule at all — the change is allowed "+
		"and recorded as governed — with the added cost that the document looks correct. Correct "+
		"the whenParam/whenValue to the param the product actually sends",
		action.Name, action.Name, floorsDocument, len(approvalRules), requireApprovalWhen,
		summariseConditions(approvalRules), summariseParams(action.Params))
}

// noFloorRuleIsUnreachable reports floor rules written for actions the grant document gives to
// nobody.
//
// The order is the whole finding: internal/policy/precedence.go asks the grant check FIRST and
// returns a deny at "a role permits the action", so a floor rule for an ungranted action is
// never evaluated. The rule is in the document, it reads correctly in review, it appears in
// evidence that the floor exists — and it is unreachable code.
//
// Two grants do not appear in this document and are therefore excluded from the finding by
// name, so an operator is not sent chasing a rule that does work: the engine's own
// L0-SuperAdmin wildcard (which reaches every action), and a grant supplied at runtime by a
// registered policy provider or a config layer. The behavioural reachability check is the one
// that sees those.
func noFloorRuleIsUnreachable(t *testing.T, held *grants.Grants, floors *grants.Floors) {
	granted := grantedActions(held)
	var unreachable []string
	for action, rules := range floors.Floors {
		if len(rules) > 0 && !granted[action] {
			unreachable = append(unreachable, fmt.Sprintf("%s (%d rule(s))", action, len(rules)))
		}
	}
	if len(unreachable) == 0 {
		return
	}
	sort.Strings(unreachable)
	t.Errorf("floors in %s holds rules for action(s) that role_actions in %s grants to no role: "+
		"%s. The grant check runs BEFORE the floor, so every request for those actions is "+
		"refused at \"a role permits the action\" and these rules are never evaluated — "+
		"unreachable code in a policy document, which reviews as a control and enforces "+
		"nothing. Either grant the action to the roles that may issue it, or delete the rules. "+
		"(A super-admin still reaches them via the engine's L0-SuperAdmin wildcard, and a "+
		"registered provider or config layer can grant an action this document does not — see "+
		"the reachability check for the engine's own answer.)",
		floorsDocument, grantsDocument, strings.Join(unreachable, ", "))
}
