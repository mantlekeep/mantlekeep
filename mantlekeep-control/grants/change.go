package grants

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// nowFunc is the clock, replaceable in a test.
var nowFunc = time.Now

// ActionGrantPolicy is the action a policy change itself is governed under.
//
// Named as data like every other action, so a deployment gates it in its floor rather than
// this file deciding. What the engine fixes is only that it IS an action — a change to who
// may do what is a change with a consequence, and the largest one in the system.
const ActionGrantPolicy = "policy.grant"

// Change is one alteration to who may do what.
//
// Deliberately small. A change that could rewrite the whole document would be a change nobody
// could review: an approver seeing "policy updated" learns nothing, while one seeing "grant
// deploy.prod to L2-Operator" can say yes or no to the thing that is actually happening.
type Change struct {
	// Role is whose authority changes.
	Role string `json:"role"`
	// Action is what they gain or lose.
	Action string `json:"action"`
	// Grant is true to add the action, false to remove it.
	Grant bool `json:"grant"`
	// Reason is why. Required: a permission change with no stated reason is one nobody can
	// review a year later, and the reason is the only part of the record that survives the
	// people who made it.
	Reason string `json:"reason"`
}

// Validate refuses a change that cannot be reviewed.
func (c Change) Validate() error {
	if strings.TrimSpace(c.Role) == "" {
		return fmt.Errorf("policy change: no role named")
	}
	if strings.TrimSpace(c.Action) == "" {
		return fmt.Errorf("policy change: no action named")
	}
	if strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf(
			"policy change: %s %q for %q needs a reason — a permission change nobody explained "+
				"is one nobody can review later", c.verb(), c.Action, c.Role)
	}
	return nil
}

func (c Change) verb() string {
	if c.Grant {
		return "granting"
	}
	return "revoking"
}

// Describe renders the change in the words an approver reads.
func (c Change) Describe() string {
	return fmt.Sprintf("%s %q %s %q: %s", c.verb(), c.Action,
		map[bool]string{true: "to", false: "from"}[c.Grant], c.Role, c.Reason)
}

// Writer applies an approved policy change to whatever holds the policy.
//
// Separate from [Source] because reading and writing have different deployments: a file
// source is read by every replica and written by none, while a database source is both. A
// source that could not be written is a legitimate deployment — the commonest one — and
// making Write part of Source would force every reader to implement it.
type Writer interface {
	// Write applies the change and returns the revision now in force.
	//
	// It runs only after the door has allowed the change. An implementation must not decide
	// anything: by the time it is called the decision exists, and a writer that second-guessed
	// it would be a policy engine underneath the policy engine.
	Write(ctx context.Context, change Change) (Revision, error)
}

// Govern submits a policy change to the door and applies it only if the door allows.
//
// # Why this exists
//
// Every other change in this system is governed. A permission change was not: policy was a
// file, and editing it granted a role an action with no decision, no approval however large
// the consequence, and nothing on the chain. That is the one gap that undoes the rest —
// somebody who can edit the grants does not need to bypass the door, because they can grant
// themselves the role that opens it, and nothing records that they did.
//
// So a policy change goes through the same door as a deployment, and lands on the same chain.
// A deployment's floor decides how hard it is: most will gate ActionGrantPolicy at their
// highest tier and require a second person, but that is the floor's call and not this
// function's.
//
// The revision BEFORE and AFTER both reach the intent, so the chain records not merely that
// policy changed but which policy became which. Without it an auditor can see that somebody
// changed something and never learn what was in force on either side.
func Govern(ctx context.Context, door mantlekeep.Submitter, actor mantlekeep.Subject,
	source Source, writer Writer, change Change) (Revision, error) {

	if err := change.Validate(); err != nil {
		return "", err
	}
	if door == nil {
		// Refused, never assumed. A policy change with no door is the ungoverned edit this
		// function exists to replace.
		return "", fmt.Errorf("policy change: no door — a change to who may do what cannot be " +
			"applied without a decision")
	}
	if writer == nil {
		return "", fmt.Errorf("policy change: this deployment's policy source is read-only")
	}

	_, _, before, err := source.Load(ctx)
	if err != nil {
		// An unreadable source is not an empty one. Applying a change onto policy we could not
		// read would be writing over something unknown.
		return "", fmt.Errorf("policy change: cannot read the policy in force: %w", err)
	}

	intent := mantlekeep.Intent{
		Action:  ActionGrantPolicy,
		Subject: actor,
		Params: map[string]any{
			"role":     change.Role,
			"target":   change.Action,
			"grant":    change.Grant,
			"reason":   change.Reason,
			"revision": string(before),
		},
		Spec: mantlekeep.IntentSpec{Goal: change.Describe()},
	}

	token, err := door.Submit(ctx, intent)
	if err != nil {
		// The door's own words. A refusal paraphrased is a refusal an operator cannot act on,
		// and a require_approval reported as a denial is a change that looks broken when it is
		// merely waiting.
		return "", err
	}
	if !token.Valid(nowFunc()) {
		return "", fmt.Errorf("policy change: the door's decision had already expired")
	}

	after, err := writer.Write(ctx, change)
	if err != nil {
		return "", fmt.Errorf("policy change: approved but not applied: %w", err)
	}
	return after, nil
}

// ChangesFrom renders the difference between two grant documents as reviewable changes.
//
// For a UI that edits a whole document: what an approver must see is not the new file but the
// alterations, one line each, in the words of [Change.Describe]. A diff of JSON is not a
// review — an approver reading it is checking a text, not a decision.
func ChangesFrom(before, after *Grants, reason string) []Change {
	var changes []Change
	for _, role := range unionRoles(before, after) {
		had, has := set(before.RoleActions[role]), set(after.RoleActions[role])
		for _, action := range sortedKeys(has) {
			if !had[action] {
				changes = append(changes, Change{Role: role, Action: action, Grant: true, Reason: reason})
			}
		}
		for _, action := range sortedKeys(had) {
			if !has[action] {
				changes = append(changes, Change{Role: role, Action: action, Grant: false, Reason: reason})
			}
		}
	}
	return changes
}

func unionRoles(before, after *Grants) []string {
	seen := map[string]bool{}
	for role := range before.RoleActions {
		seen[role] = true
	}
	for role := range after.RoleActions {
		seen[role] = true
	}
	return sortedKeys(seen)
}

func set(actions []string) map[string]bool {
	out := make(map[string]bool, len(actions))
	for _, action := range actions {
		out[action] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
