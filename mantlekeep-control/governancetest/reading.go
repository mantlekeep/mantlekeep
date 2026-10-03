package governancetest

// Reading the policy documents: the few queries the checks in documents.go ask of them, and the
// rendering that turns an answer into a sentence.
//
// Split out so documents.go holds the FINDINGS and this holds the mechanics. A reader auditing
// what the suite claims should not have to step over string formatting to find it.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// grantedActions is the set of actions role_actions gives to SOME role.
//
// The engine's L0-SuperAdmin wildcard is deliberately not folded in: it would mark every action
// as granted and make the unreachable-rule finding impossible to state. Note the engine's own
// quirk while you are here — internal/policy/grants_live.go seeds L0-SuperAdmin with "*" and
// then REPLACES it if the document lists that role, so a document granting L0-SuperAdmin an
// explicit list takes the wildcard away.
func grantedActions(held *grants.Grants) map[string]bool {
	granted := map[string]bool{}
	for _, actions := range held.RoleActions {
		for _, action := range actions {
			granted[action] = true
		}
	}
	return granted
}

func rulesOfKind(rules []grants.FloorRule, kind string) []grants.FloorRule {
	var of []grants.FloorRule
	for _, rule := range rules {
		if rule.Kind == kind {
			of = append(of, rule)
		}
	}
	return of
}

// whenMatches mirrors floorWhenMatches in internal/policy/floor.go: params[key] equals the
// trigger value, tolerating a JSON bool where the document wrote the string "true".
//
// Mirrored rather than called because that function is internal to the engine. It is duplicated
// with its source named so a divergence is one grep away — and a suite that judged a gate by a
// laxer rule than the engine applies would pass documents the door does not honour.
func whenMatches(params map[string]any, key, want string) bool {
	if params == nil {
		return false
	}
	switch value := params[key].(type) {
	case bool:
		return value && want == "true"
	case string:
		return value == want
	}
	return false
}

// summarise renders a set of action names in a stable order, so two runs of a failing suite
// produce the same sentence and a reader can diff them.
func summarise(actions map[string]bool) string {
	names := make([]string, 0, len(actions))
	for action := range actions {
		names = append(names, action)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func summariseParams(params map[string]any) string {
	if len(params) == 0 {
		return "no params at all"
	}
	pairs := make([]string, 0, len(params))
	for key, value := range params {
		pairs = append(pairs, fmt.Sprintf("%s=%v", key, value))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, " ")
}

func summariseConditions(rules []grants.FloorRule) string {
	conditions := make([]string, 0, len(rules))
	for _, rule := range rules {
		conditions = append(conditions, fmt.Sprintf("%s==%q", rule.WhenParam, rule.WhenValue))
	}
	sort.Strings(conditions)
	return strings.Join(conditions, " or ")
}
