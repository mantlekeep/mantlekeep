package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	pgpolicy "github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres"
)

// The statements, as constants. Every value the application supplies travels as a placeholder;
// nothing is ever formatted into SQL. Table names are fixed text, so a caller cannot reach
// another table through this package however it is wired.
const (
	// headQuery reads the policy in force. Unqualified table name: the deployment chooses the
	// schema through search_path, the same way its migration tool does.
	headQuery = `SELECT grants_doc, floors_doc, revision FROM mantlekeep_policy_head WHERE id`

	// lockHeadQuery reads the head AND holds it for the rest of the transaction.
	//
	// FOR UPDATE, not a bare read, and it reads the DOCUMENTS rather than just the revision so
	// that the whole read-modify-write happens inside the lock. Applying a policy change is
	// read-modify-write; computed outside the lock, two concurrent writers each read the same
	// head and the second stores a document derived from a policy that no longer exists,
	// silently reverting the first while both are told they succeeded. Optimistic concurrency
	// alone turns that into a race one of them loses — correct, but it throws away an approval
	// the door already granted, and under real contention it throws away many. Holding the row
	// for the whole step means nobody loses: sixteen operators editing at the same moment
	// serialise into sixteen changes, all applied, all recorded.
	//
	// There is exactly one row to lock, so there is no lock ordering to get wrong and no
	// deadlock to have. Policy changes are rare and human-driven; serialising them costs
	// nothing worth having.
	lockHeadQuery = `SELECT grants_doc, floors_doc, revision
	                   FROM mantlekeep_policy_head WHERE id FOR UPDATE`

	// updateHeadStatement carries the compare-and-set predicate even though the row is already
	// locked. Under the lock it cannot fail; if it ever did, the head is not what this
	// transaction read and committing would overwrite a change nobody saw. Cheap, and it makes
	// the invariant something the database enforces rather than something this file remembers.
	updateHeadStatement = `UPDATE mantlekeep_policy_head
	   SET grants_doc = $1, floors_doc = $2, revision = $3, updated_at = now()
	 WHERE id AND revision = $4`

	appendHistoryStatement = `INSERT INTO mantlekeep_policy_history
	    (parent_revision, revision, grants_doc, floors_doc,
	     change_role, change_action, change_grant, change_reason)
	 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	// seedHeadStatement creates the policy where there is none. ON CONFLICT DO NOTHING is the
	// safety: seeding can bring a policy into existence, and can never change one that already
	// exists. A bootstrap that could overwrite would be an ungoverned policy change wearing a
	// migration's clothes.
	seedHeadStatement = `INSERT INTO mantlekeep_policy_head (id, grants_doc, floors_doc, revision)
	 VALUES (true, $1, $2, $3) ON CONFLICT (id) DO NOTHING`
)

// Store is pgpolicy.Store over a Postgres database.
type Store struct {
	db *sql.DB
}

// Compile-time proof that this satisfies the seam it exists for.
var _ pgpolicy.Store = (*Store)(nil)

// New returns a Store over an existing pool.
//
// The caller owns the pool's lifetime, its sizing and its credentials — this package
// deliberately takes none of those, so the credentials a deployment brokers are handed in
// rather than discovered here.
//
// Panics on a nil pool: a policy store with no database behind it is a wiring error, better
// caught at startup than on the first governed change.
func New(db *sql.DB) *Store {
	if db == nil {
		panic("sqlstore: New requires a non-nil *sql.DB")
	}
	return &Store{db: db}
}

// Head implements pgpolicy.Store.
//
// Every failure is an error. None of them is an empty policy: empty grants deny everything, so
// a database that could not be read and one that legitimately grants nothing would be
// indistinguishable, and the first would present as a working deny-all rather than an outage.
func (s *Store) Head(ctx context.Context) (pgpolicy.Snapshot, error) {
	var (
		grantsDoc []byte
		floorsDoc []byte
		revision  string
	)
	err := s.db.QueryRowContext(ctx, headQuery).Scan(&grantsDoc, &floorsDoc, &revision)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// The tables exist and hold nothing. That is a deployment whose policy was never
		// seeded, and saying so is the difference between a page and a mystery.
		return pgpolicy.Snapshot{}, pgpolicy.ErrNoPolicy
	case err != nil:
		return pgpolicy.Snapshot{}, fmt.Errorf("sqlstore: reading the policy in force: %w", err)
	}
	return pgpolicy.Snapshot{
		GrantsDoc:      grantsDoc,
		FloorsDoc:      floorsDoc,
		StoredRevision: grants.Revision(revision),
	}, nil
}

// Update implements pgpolicy.Store: it reads the head under a row lock, hands it to apply, and
// stores the result — the whole read-modify-write in ONE transaction.
//
// Both halves of the write land or neither does. Without the transaction, a crash between the
// new head and its history row leaves a policy in force that the history cannot explain — and
// a history with a gap is not evidence, because an auditor cannot tell a gap from a deletion.
func (s *Store) Update(ctx context.Context, apply func(pgpolicy.Snapshot) (pgpolicy.Entry, error)) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: starting the policy change transaction: %w", err)
	}
	// Rolled back unless it was committed; Rollback on a committed transaction is a no-op, so
	// this is the safe shape rather than a branch on every return path.
	defer func() { _ = transaction.Rollback() }()

	var (
		grantsDoc []byte
		floorsDoc []byte
		revision  string
	)
	err = transaction.QueryRowContext(ctx, lockHeadQuery).Scan(&grantsDoc, &floorsDoc, &revision)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return pgpolicy.ErrNoPolicy
	case err != nil:
		return fmt.Errorf("sqlstore: reading the policy to change it: %w", err)
	}

	head := pgpolicy.Snapshot{
		GrantsDoc:      grantsDoc,
		FloorsDoc:      floorsDoc,
		StoredRevision: grants.Revision(revision),
	}
	entry, err := apply(head)
	if err != nil {
		// The caller could not render the change — a stored document that does not parse, most
		// likely. Returned unchanged, and nothing is written.
		return err
	}

	result, err := transaction.ExecContext(ctx, updateHeadStatement,
		entry.GrantsDoc, entry.FloorsDoc, string(entry.StoredRevision), revision)
	if err != nil {
		return fmt.Errorf("sqlstore: writing the new policy: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlstore: writing the new policy: %w", err)
	}
	if changed != 1 {
		// Unreachable while the row is locked. Reported rather than committed anyway: if the
		// head is not what this transaction read, committing would overwrite a change nobody
		// saw, which is the one outcome this whole design exists to prevent.
		return fmt.Errorf("%w: the head is no longer at %s", pgpolicy.ErrRevisionConflict, revision)
	}

	if _, err := transaction.ExecContext(ctx, appendHistoryStatement,
		string(entry.ParentRevision), string(entry.StoredRevision), entry.GrantsDoc, entry.FloorsDoc,
		entry.Change.Role, entry.Change.Action, entry.Change.Grant, entry.Change.Reason); err != nil {
		return fmt.Errorf("sqlstore: recording the policy change: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("sqlstore: committing the policy change: %w", err)
	}
	return nil
}
