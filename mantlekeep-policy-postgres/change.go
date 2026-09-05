package pgpolicy

import (
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// applyChange edits the grant document in place to say what the change says.
//
// It decides NOTHING. The door has already allowed this change by the time it is reached, so
// there is no case here that refuses one: a change that grants what is already granted, or
// revokes what was never held, is applied as the no-op it is and recorded in the history with
// its reason. A writer that refused those would be a second policy engine, sitting underneath
// the one the floor governs and visible to no chain.
//
// # Why it touches only the entry the change names
//
// It appends and removes; it never re-sorts, re-keys or re-renders anything else. That is
// what makes the revision a statement about the CONTENT rather than a change counter: a
// grant followed by its revoke leaves the document byte-identical to how it started, so it
// reports the revision it started with, and a database deployment that has churned through a
// hundred changes back to where it began can still be shown equal to the file deployment it
// was migrated from.
//
// Sorting the actions would break exactly that. It would rewrite documents on the first
// write, changing the revision of a policy that had not changed.
func applyChange(document *grants.Grants, change grants.Change) {
	if change.Grant {
		grantAction(document, change.Role, change.Action)
		return
	}
	revokeAction(document, change.Role, change.Action)
}

func grantAction(document *grants.Grants, role, action string) {
	actions := document.RoleActions[role]
	for _, held := range actions {
		if held == action {
			return // already granted; the document does not change, so neither does the revision
		}
	}
	// Appended, not inserted in order — see applyChange for why position is left alone.
	document.RoleActions[role] = append(actions, action)
}

func revokeAction(document *grants.Grants, role, action string) {
	actions, held := document.RoleActions[role]
	if !held {
		// The role has no entry. Creating an empty one would change the document — and so the
		// revision — for a change that removed nothing.
		return
	}

	remaining := make([]string, 0, len(actions))
	for _, candidate := range actions {
		if candidate != action {
			remaining = append(remaining, candidate)
		}
	}
	if len(remaining) == len(actions) {
		return // nothing was held; leave the document exactly as it was
	}

	if len(remaining) == 0 {
		// An absent role and a role with an empty action list are the same policy — both
		// grant nothing — so the document is kept in ONE shape rather than accumulating empty
		// entries that make two identical policies hash differently.
		//
		// The cost, stated plainly: a document that shipped an explicit empty list for a role
		// loses it on the first revoke, which changes the revision without changing what is
		// permitted. That is the rarer case, and the round trip that a grant and its revoke
		// return to the same revision is worth more.
		delete(document.RoleActions, role)
		return
	}
	document.RoleActions[role] = remaining
}
