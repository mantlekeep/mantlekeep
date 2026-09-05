package pgpolicy

import (
	"context"
	"errors"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// Sentinel errors. A caller branches on these with errors.Is; they are part of this
// package's contract and a [Store] implementation is expected to produce them.
var (
	// ErrNoPolicy means the store holds no policy row at all. It is an ERROR and never an
	// empty policy: empty grants deny everything, so a schema that was never applied would
	// otherwise present as a working deny-all rather than as the outage it is.
	ErrNoPolicy = errors.New("pgpolicy: the store holds no policy")

	// ErrRevisionConflict means the head moved between the read and the write — another
	// operator applied a change first. A [Store] MUST return this rather than overwriting,
	// and MUST NOT have written anything when it does. The caller re-reads and re-applies,
	// which is only safe because of that second half.
	ErrRevisionConflict = errors.New("pgpolicy: the policy changed under this write")

	// ErrCorruptDocument means a stored document is not the policy JSON it is supposed to
	// be. Also an error rather than an empty policy, and for the same reason.
	ErrCorruptDocument = errors.New("pgpolicy: stored policy document is not readable")
)

// Snapshot is the policy exactly as the store holds it: two documents, as bytes.
//
// Bytes rather than decoded structs on purpose. Decoding, normalising and hashing are the
// rules this package is responsible for and must be testable without a database; a store
// that decoded would be deciding what the policy means, and two stores could then disagree.
type Snapshot struct {
	// GrantsDoc is the grant document (role→actions, approval actions) as stored.
	GrantsDoc []byte
	// FloorsDoc is the floor document (per-action admission rules) as stored.
	FloorsDoc []byte

	// StoredRevision is the revision the store recorded alongside these documents.
	//
	// It is the CONCURRENCY TOKEN, not the answer to "which policy is this". That answer is
	// derived from the documents themselves — see [grants.RevisionOf] and revision.go —
	// because a value somebody typed, or a column somebody edited, can be stale while a hash
	// of what was actually loaded cannot.
	//
	// Keeping it in a column anyway earns its place twice: it is what an operator reads in
	// psql to see whether two replicas are serving the same policy, and it is the predicate
	// the compare-and-set write turns on. A governed write always rewrites it in the same
	// transaction as the documents, so the two can only disagree after an edit made outside
	// the door — and the next governed write, which compares against the value it READ,
	// applies cleanly and puts the column right again rather than deadlocking on it.
	StoredRevision grants.Revision
}

// Entry is one applied change: the documents that are now in force, the revision they carry,
// and the change that produced them.
//
// Who made the change is deliberately absent. [grants.Writer.Write] is handed a
// [grants.Change] and nothing else, because the actor is already recorded — [grants.Govern]
// puts it on the hash chain, with the before-revision, as part of the decision that allowed
// this write. Copying an actor into this table would create a second, unsigned answer to
// "who did this" that could disagree with the chain's.
type Entry struct {
	Snapshot
	// Change is the alteration this entry applied, reason included.
	Change grants.Change
	// ParentRevision is the revision this change was applied to.
	ParentRevision grants.Revision
}

// Store is the whole seam onto the rows. It is the only thing in this package that knows
// Postgres exists.
//
// Two methods, because there are exactly two things policy storage must do and they have
// different deployments: every replica calls Head, and only the process handling an approved
// change calls Update. A read-only deployment is a legitimate one — the commonest one — so
// nothing here forces a reader to implement a write.
type Store interface {
	// Head returns the policy currently in force.
	//
	// It returns ErrNoPolicy when there is no row, and an error for every other failure. It
	// never returns an empty Snapshot and a nil error.
	Head(ctx context.Context) (Snapshot, error)

	// Update applies a change as ONE atomic step: it reads the head, hands it to apply, and
	// stores what apply returns — with nothing able to change the head in between, and with
	// the new head and its history row landing together or not at all.
	//
	// # Why the change is computed INSIDE the store rather than handed to it
	//
	// Applying a policy change is read-modify-write. Split across two calls — read the head,
	// compute the new document, write it — there is a window between them in which another
	// operator can commit, and the second writer then stores a document computed from a policy
	// that no longer exists: the first operator's change is silently reverted while both are
	// told their change was applied.
	//
	// Closing that window with a retry alone is not enough. It works, but it is a race that has
	// to be won, and under real contention the tail of the writers exhausts its attempts and a
	// change the door already approved is thrown away. Handing the transformation INTO the
	// store lets an implementation hold the head for the whole read-modify-write, so every
	// writer succeeds on its first attempt and none is left to lose a race. Policy changes are
	// rare and human-driven; serialising them costs nothing worth having.
	//
	// An implementation that cannot hold the head — one without row locking — must instead
	// verify that the head still carries the Snapshot's revision and return ErrRevisionConflict
	// if it does not, having written NOTHING. The caller re-reads and re-applies.
	//
	// apply decides nothing about policy: it is the same approved change, rendered against
	// whatever the head turns out to hold. An error from apply aborts the update and is
	// returned unchanged.
	Update(ctx context.Context, apply func(head Snapshot) (Entry, error)) error
}
