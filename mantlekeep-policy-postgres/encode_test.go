package pgpolicy

import (
	"context"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// TestEncodeProducesWhatLoadReadsBack. Encode is the migration bridge: an operator seeds
// Postgres with what it returns and expects the database to serve the revision it was told.
// If the two ever disagreed, a migration would report success and then serve a policy with a
// different identity from the one that was proved equivalent.
func TestEncodeProducesWhatLoadReadsBack(t *testing.T) {
	t.Setenv(grants.EnvOverride, sharedGrantsJSON)
	t.Setenv(grants.FloorsEnvOverride, sharedFloorsJSON)
	t.Setenv("MANTLEKEEP_PLATFORM_POLICY", "")
	t.Setenv("MANTLEKEEP_POLICY_DIR", "")

	fileGrants, fileFloors, fileRevision, err := grants.EnvSource{}.Load(context.Background())
	if err != nil {
		t.Fatalf("loading from the file source: %v", err)
	}

	snapshot, encodedRevision, err := Encode(fileGrants, fileFloors)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encodedRevision != fileRevision {
		t.Errorf("Encode reported revision %s for the policy the file source calls %s — a "+
			"migration would be proved against a revision the database never serves",
			encodedRevision, fileRevision)
	}
	if snapshot.StoredRevision != encodedRevision {
		t.Errorf("Encode returned a snapshot carrying %s and reported %s",
			snapshot.StoredRevision, encodedRevision)
	}

	_, _, servedRevision, err := New(&fakeStore{head: snapshot, present: true}).Load(context.Background())
	if err != nil {
		t.Fatalf("loading back what Encode produced: %v", err)
	}
	if servedRevision != fileRevision {
		t.Errorf("the database serves %s for a policy the file source calls %s", servedRevision, fileRevision)
	}
}

// TestEncodeRefusesAMissingDocument. A nil document quietly treated as an empty one is a
// deny-all nobody wrote down — and, seeded into a store, one that outlives the mistake.
func TestEncodeRefusesAMissingDocument(t *testing.T) {
	if _, _, err := Encode(nil, floorsDoc(nil)); err == nil {
		t.Error("Encode accepted a missing grant document")
	}
	if _, _, err := Encode(grantsDoc(nil), nil); err == nil {
		t.Error("Encode accepted a missing floor document")
	}
}
