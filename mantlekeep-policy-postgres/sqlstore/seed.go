package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	pgpolicy "github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres"
)

// Seed brings a policy into existence where there is none. It reports whether it created one.
//
// This is the migration step: load the policy from wherever it lives today (the file source,
// most often), render it with pgpolicy.Encode, and seed what that returns. The revision Encode
// reported is the one this database will serve from now on, so the migration can be PROVED
// equivalent to the deployment it replaced rather than assumed to be.
//
// # Why it cannot overwrite
//
// It creates and never replaces: run against a database that already holds a policy it changes
// nothing and reports false. Seeding is the one write in this module that does not go through
// the door, because at the moment it runs there is no policy for a door to consult — so it is
// confined to the one case where that is true. A bootstrap that could also overwrite would be
// an ungoverned policy change wearing a migration's clothes, and the easiest way in the whole
// system to grant yourself a role.
//
// A reason is required, for the same cause [grants.Change] requires one: the genesis row is
// the first thing an auditor walking the history reads, and "where did this policy come from"
// is the question it must answer.
func (s *Store) Seed(ctx context.Context, snapshot pgpolicy.Snapshot, reason string) (bool, error) {
	if len(snapshot.GrantsDoc) == 0 || len(snapshot.FloorsDoc) == 0 {
		return false, fmt.Errorf("sqlstore: seeding needs both documents; a missing one is not an empty one")
	}
	if strings.TrimSpace(reason) == "" {
		return false, fmt.Errorf("sqlstore: seeding needs a reason — it is the only record of " +
			"where this deployment's policy came from")
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("sqlstore: starting the seed transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(ctx, seedHeadStatement,
		snapshot.GrantsDoc, snapshot.FloorsDoc, string(snapshot.StoredRevision))
	if err != nil {
		return false, fmt.Errorf("sqlstore: seeding the policy: %w", err)
	}
	created, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("sqlstore: seeding the policy: %w", err)
	}
	if created == 0 {
		return false, nil // a policy already exists; it is not this function's to change
	}

	// The genesis history row. Empty role and action because seeding is not a change to
	// anybody's authority — and present at all so that a point-in-time question about a date
	// before the first change has something to answer with.
	if _, err := transaction.ExecContext(ctx, appendHistoryStatement,
		"", string(snapshot.StoredRevision), snapshot.GrantsDoc, snapshot.FloorsDoc,
		"", "", false, reason); err != nil {
		return false, fmt.Errorf("sqlstore: recording where the policy came from: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("sqlstore: committing the seed: %w", err)
	}
	return true, nil
}

// ApplySchema creates the tables. Deliberately NOT called by this package on its own initiative — the DDL
// ships as pgpolicy.Schema so a deployment runs it through the migration tool it already has,
// rather than granting the application DDL rights on its own policy store for ever.
//
// It is exported for the one caller that legitimately wants it in code: a test, which must
// create its tables from the SAME text an operator runs rather than from SQL it invented.
func ApplySchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, pgpolicy.Schema); err != nil {
		return fmt.Errorf("sqlstore: applying the policy schema: %w", err)
	}
	return nil
}
