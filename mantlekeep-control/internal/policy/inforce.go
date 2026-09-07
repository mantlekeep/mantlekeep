package policy

import (
	"sort"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// This file answers ONE question: what policy is this engine actually evaluating against?
//
// It is deliberately read off the engine's own loaded state rather than re-read from the
// source. A surface that re-reads the file would show what the file says NOW while the door is
// still deciding on the last documents it ACCEPTED — and would do so confidently. Those two
// differ exactly when it matters most: when someone has just saved a policy the engine refused.
// A permission screen that is wrong is worse than no permission screen, because people believe
// it.

// InForce returns the documents this engine is evaluating against.
//
// Not the documents on disk: the ones in the engine. The difference is the whole point — a
// file the engine has not accepted is a file, not a law, and only one of them decides
// anything. Pair it with [RevisionInForce] to say WHICH documents these are.
//
// The role grants include the engine's own L0-SuperAdmin wildcard, which is NOT in any policy
// document. Omitting it would understate the most powerful role in the system on the one
// screen built to show who may do what.
func InForce() (*grants.Grants, *grants.Floors) {
	snapshot := ensurePolicy()

	roles := make(map[string][]string, len(snapshot.roleActions))
	for role, actions := range snapshot.roleActions {
		roles[role] = sortedActions(actions)
	}
	approvals := sortedActions(snapshot.approvalActions)

	// Copied, not aliased — and copied ALL THE WAY DOWN. The snapshot is the live law, so
	// handing a caller anything that still points into it would let a rendering bug edit the
	// policy the door enforces. A fresh outer map holding the same rule slices is not a copy,
	// and neither is a fresh rule holding the same Values slice.
	floors := &grants.Floors{Floors: make(map[string][]grants.FloorRule, len(snapshot.floors.Floors))}
	for action, rules := range snapshot.floors.Floors {
		copied := make([]grants.FloorRule, 0, len(rules))
		for _, rule := range rules {
			copied = append(copied, copyRule(rule))
		}
		floors.Floors[action] = copied
	}

	return &grants.Grants{RoleActions: roles, ApprovalActions: approvals}, floors
}

// copyRule detaches a rule from the loaded document, including the slice and map it carries.
func copyRule(rule grants.FloorRule) grants.FloorRule {
	rule.Values = append([]string(nil), rule.Values...)
	rule.WhenIn = append([]string(nil), rule.WhenIn...)
	if rule.Caps != nil {
		caps := make(map[string]string, len(rule.Caps))
		for resource, limit := range rule.Caps {
			caps[resource] = limit
		}
		rule.Caps = caps
	}
	return rule
}

func sortedActions(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for action := range set {
		out = append(out, action)
	}
	sort.Strings(out)
	return out
}
