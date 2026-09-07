package estate

import (
	"context"
	"fmt"
)

// ChangeTransformer rewrites a change before it is governed.
//
// # What this is for
//
// A deployment usually knows something about its own changes that this module does not and
// must not: how a declaration becomes the concrete thing its infrastructure applies. Doing that
// work at APPLY time — after a person has signed off — means the person approved a description
// and something else was produced from it afterwards. Anything the description was expanded
// from could have moved in between, and the record would still say "approved".
//
// So the expansion happens HERE, before the door. The change the door rules on, the change the
// approval record stores, and the change the adapter applies are then one change.
//
// # Ordering is the guarantee
//
// Transform, then govern, then apply. All three, in that order, or the seam is worthless:
//
//   - transform after the door and a person approved something that was never applied
//   - transform after the adapter and there was nothing to govern in the first place
//   - transform TWICE and the approval path applies what the transform produces NOW under an
//     approval given for what it produced THEN, which is the same failure with an audit trail
//     that says it did not happen
//
// The third is why [Manager.Approve] does not transform: it replays a change that was already
// transformed and already stored. The ordering is enforced by where this is called from rather
// than by a flag on the change — a flag can be wrong, and a caller can set it.
//
// # What this must never become
//
// This module names no tool, and neither does this interface. It says only that a change may be
// rewritten; WHAT the rewrite is belongs entirely to the implementation, which may render a
// template, ask another system to prepare work, expand a shorthand, or do nothing at all. If a
// tool's name ever appears in this file, the seam has stopped being a port.
//
// An implementation is given the change and the team it belongs to. Everything else it needs is
// its own configuration or its own vocabulary — see [Labels], which exists so a deployment can
// carry its own words on a change without this module learning any of them.
//
// # Refusal
//
// An error stops the change before the door. Nothing is submitted, nothing is recorded as
// pending, and no adapter is called. A transform that cannot produce the concrete change has
// nothing to govern, and submitting the un-expanded one would ask a person to approve a
// description whose expansion is known to be broken.
//
// An implementation must be deterministic for a given input and must not depend on the door:
// it runs before any decision exists.
type ChangeTransformer interface {
	Transform(ctx context.Context, team string, change DesiredItem) (DesiredItem, error)
}

// ChangeTransformerFunc lets a plain function be a [ChangeTransformer], for the many
// implementations that hold no state.
type ChangeTransformerFunc func(ctx context.Context, team string, change DesiredItem) (DesiredItem, error)

// Transform calls the function.
func (f ChangeTransformerFunc) Transform(ctx context.Context, team string,
	change DesiredItem) (DesiredItem, error) {

	return f(ctx, team, change)
}

// transformChange applies the configured transform, if there is one.
//
// Unset means the change passes through untouched — not "an identity transform runs", but no
// call at all. That is what makes this safe to add to a released module: a deployment that
// configures nothing behaves exactly as it did before this existed.
func (m *Manager) transformChange(ctx context.Context, team string,
	change DesiredItem) (DesiredItem, error) {

	if m.transform == nil {
		return change, nil
	}
	transformed, err := m.transform.Transform(ctx, team, change)
	if err != nil {
		return DesiredItem{}, err
	}
	// The transform may rewrite what the change IS; it may not rewrite which change this is.
	// Identity is what the door rules on, what the approval record is found by, and what the
	// reconciler compares — a transform that changed it would produce an approval for one
	// resource and an application to another, and both records would look correct.
	if transformed.key() != change.key() {
		return DesiredItem{}, fmt.Errorf(
			"the transform changed the identity of this change from %q to %q — a transform may "+
				"rewrite what a change is, never which change it is, or the door rules on one "+
				"resource and the adapter is handed another",
			change.key(), transformed.key())
	}
	return transformed, nil
}

// transformThenApply is the ORDER, written once: a change is transformed, then governed, then
// applied.
//
// Separate from [Manager.applyOne] on purpose. applyOne is the door, and it is also what the
// approval path replays — a change that has already been through here. Putting the transform
// inside applyOne would transform that replay a second time, so the split is not tidiness: it
// is the only-once guarantee, held by structure rather than by remembering.
func (m *Manager) transformThenApply(ctx context.Context, who acting, team string,
	change DesiredItem, floorRevision string) Result {

	transformed, err := m.transformChange(ctx, team, change)
	if err != nil {
		// Reported as a failure rather than a refusal, and the distinction is not cosmetic.
		// Result.Refused carries the DOOR's own words, and the door was never asked; calling
		// this a refusal would put a sentence in the door's mouth and, worse, would leave a
		// caller looking for the approval it could act on. Nobody can approve their way past a
		// transform that will not run.
		return Result{Change: change,
			Failed: fmt.Sprintf("the change was not submitted: %v", err)}
	}
	return m.applyOne(ctx, who, team, transformed, floorRevision)
}
