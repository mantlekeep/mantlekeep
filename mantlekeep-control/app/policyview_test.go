package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// The one property everything else rests on: PolicyInForce reports the ENGINE's documents, not
// the files. The engine loads once, so a file edited afterwards is a file — and a screen fed by
// the file would show it and be believed.

// A revision must be DERIVED from the documents it identifies. One that is remembered, declared
// or constant identifies nothing: two different policies would share it, and the whole point of
// putting it on the page is that two replicas serving different policy can be told apart.
func TestTheRevisionIsDerivedFromTheDocumentsItReports(t *testing.T) {
	held, floors, revision, err := (PolicyInForce{}).Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Recomputed here, independently, from the documents that came back.
	recomputed := grants.RevisionOf(marshal(t, held), marshal(t, floors))
	if revision != recomputed {
		t.Fatalf("the reported revision %q does not identify the documents reported with it "+
			"(%q) — a revision that is not derived from the policy names the wrong policy",
			revision, recomputed)
	}
	if revision == "" {
		t.Fatal("the revision is empty, so the page can say nothing about which policy is in force")
	}
}

// The staleness test. The engine has already loaded by the time this runs; pointing the
// ENVIRONMENT at a different floor document changes what a file-reading loader would report and
// must change NOTHING about what the door is enforcing — so PolicyInForce must not move either.
//
// Wire PolicyInForce to grants.EnvSource and this fails, which is the point: the two loaders
// answer different questions, and a permission screen that asked the file's would go stale
// silently while continuing to answer confidently.
func TestTheRevisionFollowsTheEngineAndNotTheFiles(t *testing.T) {
	_, _, before, err := (PolicyInForce{}).Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, _, fileBefore, err := (grants.EnvSource{}).Load(context.Background())
	if err != nil {
		t.Fatalf("load from the environment: %v", err)
	}

	// A floor document nothing in this build carries, set AFTER the engine loaded.
	t.Setenv(grants.FloorsEnvOverride,
		`{"floors":{"test.stale":[{"kind":"allowlist","param":"x","values":["y"],`+
			`"message":"a rule the engine never loaded"}]}}`)

	_, _, fileAfter, err := (grants.EnvSource{}).Load(context.Background())
	if err != nil {
		t.Fatalf("re-load from the environment: %v", err)
	}
	if fileAfter == fileBefore {
		t.Fatalf("the environment override changed nothing (%q) — this test cannot prove "+
			"anything until it does", fileAfter)
	}

	_, _, after, err := (PolicyInForce{}).Load(context.Background())
	if err != nil {
		t.Fatalf("re-load: %v", err)
	}
	if after != before {
		t.Fatalf("the reported revision moved from %q to %q because a FILE changed, while the "+
			"door is still enforcing what it loaded — a page fed by this would show a policy "+
			"nobody is deciding under", before, after)
	}
}

func marshal(t *testing.T, document any) []byte {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshalling a policy document: %v", err)
	}
	return encoded
}
