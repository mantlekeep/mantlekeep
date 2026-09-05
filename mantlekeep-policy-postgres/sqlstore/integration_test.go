package sqlstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	"github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres"
	"github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres/sqlstore"
)

// dsnEnv names a THROWAWAY Postgres database to run the integration tests against. The suite
// drops and recreates the policy tables, so it must never point at anything that matters.
//
//	docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=policy postgres:17
//	MANTLEKEEP_POSTGRES_TEST_DSN='postgres://postgres:policy@127.0.0.1:5432/postgres?sslmode=disable' \
//	  go test ./sqlstore/...
//
// Unset, the suite skips. That is the split this module is built around: every RULE about
// policy is decided in the parent package with a fake and runs on every CI machine, and the
// SQL that makes those rules true is proved here, against the only thing that can prove it.
// A fake cannot demonstrate that Postgres serialises two writers; only Postgres can.
const dsnEnv = "MANTLEKEEP_POSTGRES_TEST_DSN"

const (
	testGrantsJSON = `{"role_actions":{"L2-Operator":["job.run","deploy.dev"]},` +
		`"approval_actions":["deploy.prod.approve"]}`
	testFloorsJSON = `{"floors":{"deploy.prod":[{"kind":"allowlist","param":"env","values":["prod"]}]}}`
)

// TestStoreAgainstRealPostgres proves the SQL, not the rules: the transaction, the
// compare-and-set under real concurrency, and the append-only history.
func TestStoreAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("no database: set %s to a THROWAWAY Postgres to run these", dsnEnv)
	}

	db, err := sqlstore.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	t.Run("an empty schema is an error, not an empty policy", func(t *testing.T) {
		store := freshStore(t, db)

		snapshot, err := store.Head(context.Background())
		if !errors.Is(err, pgpolicy.ErrNoPolicy) {
			t.Fatalf("Head on an unseeded database: got (%+v, %v), want ErrNoPolicy", snapshot, err)
		}

		_, _, revision, err := pgpolicy.New(store).Load(context.Background())
		if err == nil {
			t.Fatal("Load on an unseeded database succeeded; a deployment that never loaded its " +
				"policy would present as a deliberate deny-all")
		}
		if revision != "" {
			t.Errorf("a failed load handed back revision %q", revision)
		}
	})

	t.Run("a real database serves the revision the file source serves", func(t *testing.T) {
		// The property the whole revision design exists for, proved end to end against
		// Postgres: the same policy in a file and in a database is the same revision, so a
		// migration between them can be shown equivalent rather than assumed to be.
		t.Setenv(grants.EnvOverride, testGrantsJSON)
		t.Setenv(grants.FloorsEnvOverride, testFloorsJSON)
		t.Setenv("MANTLEKEEP_PLATFORM_POLICY", "")
		t.Setenv("MANTLEKEEP_POLICY_DIR", "")

		fileGrants, fileFloors, fileRevision, err := grants.EnvSource{}.Load(context.Background())
		if err != nil {
			t.Fatalf("loading from the file source: %v", err)
		}
		snapshot, encoded, err := pgpolicy.Encode(fileGrants, fileFloors)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		store := freshStore(t, db)
		created, err := store.Seed(context.Background(), snapshot, "migrated from MANTLEKEEP_POLICY_GRANTS")
		if err != nil {
			t.Fatalf("seeding: %v", err)
		}
		if !created {
			t.Fatal("Seed reported that a policy already existed in a freshly created schema")
		}

		_, _, served, err := pgpolicy.New(store).Load(context.Background())
		if err != nil {
			t.Fatalf("loading from Postgres: %v", err)
		}
		if served != fileRevision || served != encoded {
			t.Errorf("the same policy has three identities:\n  file source: %s\n  Encode: %s\n"+
				"  Postgres: %s", fileRevision, encoded, served)
		}
	})

	t.Run("seeding never overwrites a policy that already exists", func(t *testing.T) {
		store := seededStore(t, db)
		_, _, before, err := pgpolicy.New(store).Load(context.Background())
		if err != nil {
			t.Fatalf("loading: %v", err)
		}

		other, _, err := pgpolicy.Encode(&grants.Grants{RoleActions: map[string][]string{
			"L0-SuperAdmin": {"policy.grant"}}}, &grants.Floors{})
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		created, err := store.Seed(context.Background(), other, "trying to bootstrap over a live policy")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		if created {
			t.Error("Seed overwrote a policy that already existed — an ungoverned policy change " +
				"wearing a migration's clothes")
		}

		_, _, after, err := pgpolicy.New(store).Load(context.Background())
		if err != nil {
			t.Fatalf("loading: %v", err)
		}
		if after != before {
			t.Errorf("the policy changed under a re-seed: %s became %s", before, after)
		}
	})

	t.Run("a change that cannot be rendered writes nothing", func(t *testing.T) {
		// Update hands the head to the caller and stores what comes back. When the caller
		// cannot produce an entry — a stored document that does not parse, most often — the
		// transaction must roll back whole: no new head, and no history row claiming a change
		// that never happened.
		store := seededStore(t, db)
		head, err := store.Head(context.Background())
		if err != nil {
			t.Fatalf("Head: %v", err)
		}

		refused := errors.New("the caller could not render the change")
		err = store.Update(context.Background(), func(pgpolicy.Snapshot) (pgpolicy.Entry, error) {
			return pgpolicy.Entry{}, refused
		})
		if !errors.Is(err, refused) {
			t.Fatalf("Update: got %v, want the caller's own error", err)
		}

		after, err := store.Head(context.Background())
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		if after.StoredRevision != head.StoredRevision {
			t.Errorf("an abandoned update still changed the head: %s became %s",
				head.StoredRevision, after.StoredRevision)
		}
		if rows := countHistory(t, db); rows != 1 {
			t.Errorf("an abandoned update left %d history rows, want just the genesis row", rows)
		}
	})

	t.Run("concurrent operators do not lose each other's changes", func(t *testing.T) {
		// The test a fake cannot do. Sixteen operators grant sixteen different actions at the
		// same moment through the same one-row head. Every one of them must survive: a write
		// that read a policy another writer has since replaced must be refused, re-read and
		// re-applied, not silently stored over the top.
		const operators = 16

		store := seededStore(t, db)
		policy := pgpolicy.New(store)

		var waitGroup sync.WaitGroup
		failures := make(chan error, operators)
		start := make(chan struct{})

		for operator := range operators {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start // release them together, so the writes genuinely overlap
				_, err := policy.Write(context.Background(), grants.Change{
					Role:   "L2-Operator",
					Action: fmt.Sprintf("job.run.%02d", operator),
					Grant:  true,
					Reason: fmt.Sprintf("operator %d, editing at the same moment as the others", operator),
				})
				if err != nil {
					failures <- fmt.Errorf("operator %d: %w", operator, err)
				}
			}()
		}
		close(start)
		waitGroup.Wait()
		close(failures)

		for err := range failures {
			t.Errorf("a change the door approved was not applied: %v", err)
		}

		loaded, _, _, err := policy.Load(context.Background())
		if err != nil {
			t.Fatalf("loading the result: %v", err)
		}
		held := map[string]bool{}
		for _, action := range loaded.RoleActions["L2-Operator"] {
			held[action] = true
		}
		for operator := range operators {
			action := fmt.Sprintf("job.run.%02d", operator)
			if !held[action] {
				t.Errorf("operator %d's change was lost: %q is not granted", operator, action)
			}
		}
		if !held["job.run"] || !held["deploy.dev"] {
			t.Errorf("the policy that was there before the race was overwritten: %v",
				loaded.RoleActions["L2-Operator"])
		}

		// One row per change, plus the genesis row — and the parent of each is the revision the
		// one before it produced. A history that does not chain is a history with a gap, and an
		// auditor cannot tell a gap from a deletion.
		if rows := countHistory(t, db); rows != operators+1 {
			t.Errorf("history holds %d rows, want %d", rows, operators+1)
		}
		assertHistoryChains(t, db)
	})

	t.Run("the stored documents migrate back to a file deployment", func(t *testing.T) {
		// The README documents the way OUT of this module as well as the way in. An escape
		// route nobody exercised is one a deployment discovers is broken at the worst moment,
		// so it is proved here: write the two columns to files, point the file source at them,
		// and the revision must be the one Postgres was serving.
		store := seededStore(t, db)
		policy := pgpolicy.New(store)

		if _, err := policy.Write(context.Background(), grants.Change{
			Role: "L3-Approver", Action: "deploy.prod", Grant: true, Reason: "release window"}); err != nil {
			t.Fatalf("granting: %v", err)
		}
		_, _, served, err := policy.Load(context.Background())
		if err != nil {
			t.Fatalf("loading: %v", err)
		}

		head, err := store.Head(context.Background())
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		directory := t.TempDir()
		grantsPath := filepath.Join(directory, "grants.json")
		floorsPath := filepath.Join(directory, "floors.json")
		if err := os.WriteFile(grantsPath, head.GrantsDoc, 0o600); err != nil {
			t.Fatalf("exporting the grant document: %v", err)
		}
		if err := os.WriteFile(floorsPath, head.FloorsDoc, 0o600); err != nil {
			t.Fatalf("exporting the floor document: %v", err)
		}

		t.Setenv(grants.EnvOverride, grantsPath)
		t.Setenv(grants.FloorsEnvOverride, floorsPath)
		t.Setenv("MANTLEKEEP_PLATFORM_POLICY", "")
		t.Setenv("MANTLEKEEP_POLICY_DIR", "")

		_, _, exported, err := grants.EnvSource{}.Load(context.Background())
		if err != nil {
			t.Fatalf("the file source could not read the exported documents: %v", err)
		}
		if exported != served {
			t.Errorf("the exported policy is not the one Postgres was serving:\n"+
				"  Postgres: %s\n  files:    %s", served, exported)
		}
	})

	t.Run("history answers what the policy said at a point in time", func(t *testing.T) {
		store := seededStore(t, db)
		policy := pgpolicy.New(store)

		_, _, atSeed, err := policy.Load(context.Background())
		if err != nil {
			t.Fatalf("loading: %v", err)
		}
		if _, err := policy.Write(context.Background(), grants.Change{
			Role: "L2-Operator", Action: "deploy.prod", Grant: true, Reason: "release window"}); err != nil {
			t.Fatalf("granting: %v", err)
		}
		if _, err := policy.Write(context.Background(), grants.Change{
			Role: "L2-Operator", Action: "deploy.prod", Grant: false, Reason: "window closed"}); err != nil {
			t.Fatalf("revoking: %v", err)
		}

		// The permission that existed briefly and was quietly removed — the thing an auditor is
		// actually hunting, and the thing a current-state-only store cannot show at all.
		var document []byte
		err = db.QueryRow(`SELECT grants_doc FROM mantlekeep_policy_history
		                    WHERE change_action = 'deploy.prod' AND change_grant
		                    ORDER BY seq DESC LIMIT 1`).Scan(&document)
		if err != nil {
			t.Fatalf("asking the history what the policy said: %v", err)
		}
		if !containsAction(t, document, "L2-Operator", "deploy.prod") {
			t.Errorf("the history does not show the permission that was granted and removed: %s", document)
		}

		// And the current policy is back where it started, revision included.
		_, _, now, err := policy.Load(context.Background())
		if err != nil {
			t.Fatalf("loading: %v", err)
		}
		if now != atSeed {
			t.Errorf("a grant and its revoke did not return to %s: the policy is at %s", atSeed, now)
		}
	})
}
