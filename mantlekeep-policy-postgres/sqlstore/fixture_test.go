package sqlstore_test

// The fixture the integration tests run against: a policy schema built from the SHIPPED DDL,
// and the queries that read the history back. Kept apart from the assertions so that what each
// test is PROVING is not buried in the plumbing that gets it a database.

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	"github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres"
	"github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres/sqlstore"
)

// freshStore drops the policy tables and recreates them from the SHIPPED DDL — the same text
// an operator runs, so this suite cannot pass against a schema it invented for itself.
func freshStore(t *testing.T, db *sql.DB) *sqlstore.Store {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS mantlekeep_policy_history, mantlekeep_policy_head`); err != nil {
		t.Fatalf("clearing the test schema: %v", err)
	}
	if err := sqlstore.ApplySchema(context.Background(), db); err != nil {
		t.Fatalf("applying the shipped schema: %v", err)
	}
	return sqlstore.New(db)
}

// seededStore is a fresh schema holding the test policy.
func seededStore(t *testing.T, db *sql.DB) *sqlstore.Store {
	t.Helper()
	store := freshStore(t, db)
	snapshot := pgpolicy.Snapshot{
		GrantsDoc: []byte(testGrantsJSON),
		FloorsDoc: []byte(testFloorsJSON),
	}
	normalised, revision, err := decodeThroughEncode(snapshot)
	if err != nil {
		t.Fatalf("rendering the test policy: %v", err)
	}
	normalised.StoredRevision = revision
	if _, err := store.Seed(context.Background(), normalised, "the test policy"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	return store
}

// decodeThroughEncode renders raw documents in the form the store holds them, through the
// package's own Encode so the test cannot disagree with the code about what canonical means.
func decodeThroughEncode(snapshot pgpolicy.Snapshot) (pgpolicy.Snapshot, grants.Revision, error) {
	var (
		policyGrants grants.Grants
		policyFloors grants.Floors
	)
	if err := json.Unmarshal(snapshot.GrantsDoc, &policyGrants); err != nil {
		return pgpolicy.Snapshot{}, "", err
	}
	if err := json.Unmarshal(snapshot.FloorsDoc, &policyFloors); err != nil {
		return pgpolicy.Snapshot{}, "", err
	}
	return pgpolicy.Encode(&policyGrants, &policyFloors)
}

func countHistory(t *testing.T, db *sql.DB) int {
	t.Helper()
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM mantlekeep_policy_history`).Scan(&rows); err != nil {
		t.Fatalf("counting history: %v", err)
	}
	return rows
}

// assertHistoryChains walks the history in the order the changes landed and checks each row was
// applied to the revision the row before it produced.
func assertHistoryChains(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT seq, parent_revision, revision FROM mantlekeep_policy_history ORDER BY seq`)
	if err != nil {
		t.Fatalf("reading history: %v", err)
	}
	defer func() { _ = rows.Close() }()

	previous := ""
	first := true
	for rows.Next() {
		var (
			seq             int64
			parent, current string
		)
		if err := rows.Scan(&seq, &parent, &current); err != nil {
			t.Fatalf("reading history: %v", err)
		}
		if !first && parent != previous {
			t.Errorf("history row %d was applied to %q, but the row before it produced %q — "+
				"a write stored a document computed from a policy that no longer existed",
				seq, parent, previous)
		}
		previous, first = current, false
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading history: %v", err)
	}
}

func containsAction(t *testing.T, document []byte, role, action string) bool {
	t.Helper()
	var decoded grants.Grants
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("the history holds a document that does not parse: %v", err)
	}
	for _, held := range decoded.RoleActions[role] {
		if held == action {
			return true
		}
	}
	return false
}
