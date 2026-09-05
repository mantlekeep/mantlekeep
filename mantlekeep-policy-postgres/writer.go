package pgpolicy

import (
	"context"
	"errors"
	"fmt"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// maxWriteAttempts bounds the re-read-and-reapply loop below.
//
// It is a SAFETY NET, not the mechanism. A [Store] that can hold the head for the whole
// read-modify-write — the Postgres one does, with a row lock — never conflicts, so this loop
// never runs a second time. It exists for a store that cannot, and for the serialisation
// failures a stricter isolation level can raise.
//
// Bounded, because an unbounded retry against a conflict that never clears is a policy change
// that hangs rather than fails — and a hung governed change is worse than a failed one: the
// approval is spent and nobody is told.
const maxWriteAttempts = 5

// Write implements [grants.Writer] over the store.
//
// It runs only after the door has allowed the change, and it decides NOTHING. There is no
// branch here that refuses a change on its merits — no "that role does not exist", no "that
// action is already granted", no re-validation of the reason. [grants.Change.Validate] and the
// floor have already run, at the door, where a refusal is recorded on the chain. A refusal
// invented here would be a policy engine underneath the policy engine: invisible to the floor
// that is supposed to govern it, and absent from the audit trail entirely.
//
// # How two operators editing at once are kept from overwriting each other
//
// All three of the available mechanisms are used, because they answer different halves of the
// question:
//
//   - A TRANSACTION spanning the whole read-modify-write. This is the one that does the work.
//     The change is computed INSIDE [Store.Update], against a head the store holds for the
//     duration, so there is no window in which another operator can commit between the read
//     and the write — and therefore no race for a writer to lose. Both operators' changes land,
//     in the order they arrived, each on its own revision.
//
//   - OPTIMISTIC CONCURRENCY on the revision, as the store's contract and as the assertion the
//     Postgres implementation still makes under its lock. It is what a store that cannot hold
//     the head must fall back on, and it is what turns "the head moved" into a refusal that
//     wrote nothing rather than an overwrite that reported success.
//
//   - An APPEND-ONLY HISTORY, so that where one change supersedes another BOTH survive. A store
//     holding only current state can say what the policy IS; an audit asks what it SAID on the
//     fourteenth, and the thing an auditor is hunting is the permission that existed for three
//     days and was quietly removed. That is worth one row per change.
//
// On a conflict this re-READS and re-APPLIES rather than failing. The loser of a race holds a
// change the door approved, and applying it to the policy as it now stands keeps both
// operators' changes; failing it instead throws away a live approval because somebody else was
// quicker, which teaches operators to retry by hand and eventually to edit the rows directly.
// Re-applying is not a decision — it is the same approved change, one version later.
func (p *Policy) Write(ctx context.Context, change grants.Change) (grants.Revision, error) {
	var lastConflict error

	for attempt := 0; attempt < maxWriteAttempts; attempt++ {
		revision, err := p.writeOnce(ctx, change)
		switch {
		case err == nil:
			return revision, nil
		case errors.Is(err, ErrRevisionConflict):
			// The head moved under a store that could not hold it. Nothing was written — that
			// is part of Update's contract — so re-reading is safe.
			lastConflict = err
		default:
			return "", err
		}
	}

	return "", fmt.Errorf(
		"pgpolicy: gave up applying an approved policy change after %d attempts — the policy "+
			"is being changed faster than this write can follow it: %w", maxWriteAttempts, lastConflict)
}

// writeOnce is one attempt: one atomic read-modify-write through the store.
func (p *Policy) writeOnce(ctx context.Context, change grants.Change) (grants.Revision, error) {
	var applied grants.Revision

	err := p.store.Update(ctx, func(head Snapshot) (Entry, error) {
		decoded, err := decode(head)
		if err != nil {
			// Applying a change onto policy that could not be read would be writing over
			// something unknown. An unreadable store is not an empty one.
			return Entry{}, err
		}

		applyChange(decoded.grants, change)

		next, revision, err := encode(decoded)
		if err != nil {
			return Entry{}, err
		}
		applied = revision
		return Entry{Snapshot: next, Change: change, ParentRevision: head.StoredRevision}, nil
	})
	if err != nil {
		return "", err
	}
	return applied, nil
}
