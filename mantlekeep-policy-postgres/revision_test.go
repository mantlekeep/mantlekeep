package pgpolicy

import (
	"context"
	"reflect"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// The policy both deployments hold. Written once, in the JSON an operator would author, so
// the file deployment and the database deployment are demonstrably given the SAME policy and
// not two hand-copied versions of it.
const (
	sharedGrantsJSON = `{"role_actions":{"L2-Operator":["deploy.dev","job.run"],` +
		`"L3-Approver":["deploy.prod"]},"approval_actions":["deploy.prod.approve"]}`
	sharedFloorsJSON = `{"floors":{"deploy.prod":[{"kind":"require_approval_when",` +
		`"whenParam":"env","whenValue":"prod","role":"L3-Approver"}]}}`
)

// TestIdenticalPolicyHasTheSameRevisionWhateverHoldsIt is the property the whole revision
// design exists for: a file deployment and a database deployment serving the same policy must
// report the same revision.
//
// It is what makes a migration between the two PROVABLE. Without it, an operator moving
// policy from files into Postgres can only assert that nothing changed; with it they can
// compare two sixteen-character strings and know.
func TestIdenticalPolicyHasTheSameRevisionWhateverHoldsIt(t *testing.T) {
	// The file deployment: the core's own source, reading the documents from the environment.
	// The layering env vars are cleared so this is the policy under test and not whatever the
	// machine running the test happens to have set.
	t.Setenv(grants.EnvOverride, sharedGrantsJSON)
	t.Setenv(grants.FloorsEnvOverride, sharedFloorsJSON)
	t.Setenv("MANTLEKEEP_PLATFORM_POLICY", "")
	t.Setenv("MANTLEKEEP_POLICY_DIR", "")

	fileGrants, fileFloors, fileRevision, err := grants.EnvSource{}.Load(context.Background())
	if err != nil {
		t.Fatalf("the file source could not load the policy: %v", err)
	}

	// The database deployment: the same two documents, as bytes, in the store.
	store := &fakeStore{
		head: Snapshot{
			GrantsDoc: []byte(sharedGrantsJSON),
			FloorsDoc: []byte(sharedFloorsJSON),
			// Deliberately NOT the real revision. If the loader ever returned the column
			// instead of deriving from the content, this test fails — which is the point.
			StoredRevision: "0000000000000000",
		},
		present: true,
	}

	dbGrants, dbFloors, dbRevision, err := New(store).Load(context.Background())
	if err != nil {
		t.Fatalf("the database source could not load the policy: %v", err)
	}

	if dbRevision != fileRevision {
		t.Errorf("same policy, different revision:\n  file source: %s\n  database source: %s\n"+
			"a migration between the two can no longer be proved equivalent",
			fileRevision, dbRevision)
	}
	if !reflect.DeepEqual(dbGrants, fileGrants) {
		t.Errorf("same policy, different grant documents:\n  file: %+v\n  database: %+v",
			fileGrants, dbGrants)
	}
	if !reflect.DeepEqual(dbFloors, fileFloors) {
		t.Errorf("same policy, different floor documents:\n  file: %+v\n  database: %+v",
			fileFloors, dbFloors)
	}
}

// TestRevisionIgnoresHowTheDocumentWasWritten proves the revision is a statement about the
// policy and not about the bytes: reordered keys and different whitespace are the same policy
// and must report the same revision. jsonb does exactly this to a document on the way into
// Postgres, so a revision that was sensitive to it would change the moment a policy was
// stored.
func TestRevisionIgnoresHowTheDocumentWasWritten(t *testing.T) {
	tidy := &fakeStore{present: true, head: Snapshot{
		GrantsDoc: []byte(`{"role_actions":{"L2-Operator":["deploy.dev"]},"approval_actions":[]}`),
		FloorsDoc: []byte(`{"floors":{}}`),
	}}
	untidy := &fakeStore{present: true, head: Snapshot{
		GrantsDoc: []byte("{\n  \"approval_actions\" : [],\n  \"role_actions\":{ \"L2-Operator\" : [ \"deploy.dev\" ] }\n}"),
		FloorsDoc: []byte(`{ "floors" : { } }`),
	}}

	_, _, tidyRevision, err := New(tidy).Load(context.Background())
	if err != nil {
		t.Fatalf("loading the tidy document: %v", err)
	}
	_, _, untidyRevision, err := New(untidy).Load(context.Background())
	if err != nil {
		t.Fatalf("loading the untidy document: %v", err)
	}

	if tidyRevision != untidyRevision {
		t.Errorf("the same policy reported two revisions (%s and %s) because it was written "+
			"differently; the revision must identify the policy, not the formatting",
			tidyRevision, untidyRevision)
	}
}

// TestGrantThenRevokeReturnsToTheStartingRevision proves the writer touches only the entry a
// change names.
//
// The revision must be a check on the content, not a change counter. A store that had churned
// through a grant and its revoke holds exactly the policy it started with, and must say so —
// otherwise it can never again be shown equal to the file deployment it was migrated from.
func TestGrantThenRevokeReturnsToTheStartingRevision(t *testing.T) {
	// Deliberately NOT in sorted order. A writer that tidied the document as it went would
	// change the revision of a policy nobody had changed, and this is what catches it.
	store := &fakeStore{}
	store.seed(t,
		grantsDoc(map[string][]string{"L2-Operator": {"job.run", "deploy.dev"}}),
		floorsDoc(nil))
	policy := New(store)

	_, _, before, err := policy.Load(context.Background())
	if err != nil {
		t.Fatalf("loading the starting policy: %v", err)
	}

	granted, err := policy.Write(context.Background(), grants.Change{
		Role: "L2-Operator", Action: "deploy.prod", Grant: true, Reason: "on call this week"})
	if err != nil {
		t.Fatalf("applying the grant: %v", err)
	}
	if granted == before {
		t.Fatalf("granting an action left the revision at %s — a change that changed nothing "+
			"is not a change that was applied", before)
	}

	revoked, err := policy.Write(context.Background(), grants.Change{
		Role: "L2-Operator", Action: "deploy.prod", Grant: false, Reason: "rotation over"})
	if err != nil {
		t.Fatalf("applying the revoke: %v", err)
	}

	if revoked != before {
		t.Errorf("a grant and its revoke did not return to the starting revision:\n"+
			"  started at: %s\n  ended at:   %s\nthe writer rewrote something the change did not name",
			before, revoked)
	}
}
