package sqlstore

import (
	"context"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres"
)

// These run everywhere, with no server. What they cover is the wiring a misconfiguration
// breaks at startup — the failures that must not wait until the first governed decision.

// TestNewRefusesAStoreWithNoDatabaseBehindIt: a nil pool is a wiring error, and it surfaces as
// a policy change that was approved and then lost if it is not caught here.
func TestNewRefusesAStoreWithNoDatabaseBehindIt(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New(nil) returned a Store; the failure moves to the first governed change")
		}
	}()
	New(nil)
}

// TestOpenRefusesAnEmptyDataSourceName. Defaulting to localhost would connect a policy store
// somewhere other than the operator meant — the worst possible thing to be wrong about.
func TestOpenRefusesAnEmptyDataSourceName(t *testing.T) {
	for _, dsn := range []string{"", "   "} {
		if _, err := Open(context.Background(), dsn); err == nil {
			t.Errorf("Open(%q) succeeded", dsn)
		}
	}
}

// TestTheShippedSchemaCreatesWhatTheStatementsAddress. The DDL and the SQL are written in two
// files and must agree; if they drift, every query fails at runtime against a database that
// migrated cleanly.
func TestTheShippedSchemaCreatesWhatTheStatementsAddress(t *testing.T) {
	for _, table := range []string{"mantlekeep_policy_head", "mantlekeep_policy_history"} {
		if !strings.Contains(pgpolicy.Schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("the shipped schema does not create %s, which the statements in this "+
				"package address", table)
		}
	}
	for _, column := range []string{"grants_doc", "floors_doc", "revision", "parent_revision",
		"change_role", "change_action", "change_grant", "change_reason"} {
		if !strings.Contains(pgpolicy.Schema, column) {
			t.Errorf("the shipped schema declares no %s column", column)
		}
	}
}
