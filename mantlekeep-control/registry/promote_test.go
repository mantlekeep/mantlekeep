package registry

import (
	"context"
	"testing"
)

// TestPromote_ArtifactUp_SourceEvidenceRidesAlong proves the host model: SIT builds
// from git (binary digest + git source provenance); promotion carries the SAME
// artifact up (no rebuild) AND the git commit evidence travels with it, marked
// promotedFrom. Prod runs locked bytes; the evidence still points to auditable source.
func TestPromote_ArtifactUp_SourceEvidenceRidesAlong(t *testing.T) {
	ctx := context.Background()
	sit := New(newFakeStore(), "sit", LooseDev)
	uat := New(newFakeStore(), "uat", SealedProd)

	cloner := func(_ context.Context, _, ref string) ([]byte, string, []byte, error) {
		return []byte("built@" + ref), "deadbeef", nil, nil
	}
	if _, err := sit.Ingest(ctx, GitSource{Clone: cloner},
		SourceRequest{Repo: "forgejo/app", Ref: "v1.0"},
		"app", "tool", "App", "alice", "v1.0", nil); err != nil {
		t.Fatalf("sit ingest: %v", err)
	}
	sitEntry, _, _ := sit.Get(ctx, "app")
	sitVer := sitEntry.Versions[0]

	if err := (LocalPromoter{Target: uat}).Promote(ctx, sitEntry, sitVer); err != nil {
		t.Fatalf("promote: %v", err)
	}

	upEntry, ok, _ := uat.Get(ctx, "app")
	if !ok || len(upEntry.Versions) != 1 {
		t.Fatalf("uat should hold the promoted version")
	}
	up := upEntry.Versions[0]
	if up.Ref != sitVer.Ref {
		t.Fatalf("upper env MUST run the same digest (no rebuild): sit=%s uat=%s", sitVer.Ref, up.Ref)
	}
	if up.Env != "uat" || up.Status != StatusDraft {
		t.Fatalf("promoted artifact should land as a uat draft for uat's own gate, got env=%s status=%s", up.Env, up.Status)
	}
	if up.Provenance["source"] != "git" || up.Provenance["commit"] != "deadbeef" {
		t.Fatalf("source evidence (git commit) must ride up, got %v", up.Provenance)
	}
	if up.Provenance["promotedFrom"] != "sit" {
		t.Fatalf("should be marked promotedFrom sit (promoted, not rebuilt), got %v", up.Provenance)
	}
}
