package registry

import (
	"context"
	"strings"
	"testing"
)

func TestUploadSource_ContentAddresses(t *testing.T) {
	art, err := UploadSource{}.Fetch(context.Background(), SourceRequest{
		Data: []byte("wasm-bytes"), Opts: map[string]string{"by": "alice"},
	})
	if err != nil {
		t.Fatalf("upload fetch: %v", err)
	}
	if !strings.HasPrefix(art.Digest, "sha256:") {
		t.Fatalf("want sha256 digest, got %q", art.Digest)
	}
	if art.Provenance["source"] != "upload" || art.Provenance["by"] != "alice" {
		t.Fatalf("bad provenance: %v", art.Provenance)
	}
	if _, err := (UploadSource{}).Fetch(context.Background(), SourceRequest{}); err == nil {
		t.Fatal("empty upload should error")
	}
}

func TestGitSource_InjectedFetcher(t *testing.T) {
	// the git shared-lib model: repo@ref, ref is the version, commit is provenance,
	// and a descriptor in the repo generates the template.
	fetcher := func(_ context.Context, repo, ref string) ([]byte, string, []byte, error) {
		return []byte("tree@" + ref), "abc1234", []byte(`{"summary":"from repo"}`), nil
	}
	src := GitSource{Fetcher: fetcher}
	art, err := src.Fetch(context.Background(), SourceRequest{Repo: "forgejo/my-lib", Ref: "v1.2"})
	if err != nil {
		t.Fatalf("git fetch: %v", err)
	}
	if art.Provenance["source"] != "git" || art.Provenance["repo"] != "forgejo/my-lib" ||
		art.Provenance["ref"] != "v1.2" || art.Provenance["commit"] != "abc1234" {
		t.Fatalf("bad git provenance: %v", art.Provenance)
	}
	if string(art.Manifest) != `{"summary":"from repo"}` {
		t.Fatalf("descriptor should generate the template, got %q", art.Manifest)
	}
}

func TestIngest_Git_GeneratesTemplateFromRepo(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "sit", LooseDev)
	fetcher := func(_ context.Context, _, ref string) ([]byte, string, []byte, error) {
		return []byte("lib@" + ref), "c0ffee", []byte(`{"summary":"scanner lib"}`), nil
	}
	// caller supplies NO manifest → the repo descriptor is used
	v, err := r.Ingest(ctx, GitSource{Fetcher: fetcher}, SourceRequest{Repo: "forgejo/scan", Ref: "v2.0"},
		"scan-lib", "tool", "Scan Lib", "alice", "v2.0", nil)
	if err != nil {
		t.Fatalf("ingest git: %v", err)
	}
	e, _, _ := r.Get(ctx, "scan-lib")
	if string(e.Versions[0].Manifest) != `{"summary":"scanner lib"}` {
		t.Fatalf("template should be generated from the repo, got %q", e.Versions[0].Manifest)
	}
	if v.Provenance["commit"] != "c0ffee" {
		t.Fatalf("want git provenance on the draft, got %v", v.Provenance)
	}
}

func TestGitSource_NoFetcher(t *testing.T) {
	if _, err := (GitSource{}).Fetch(context.Background(), SourceRequest{Repo: "x"}); err == nil {
		t.Fatal("git source with no injected fetcher should error")
	}
}

func TestIngest_UploadBecomesDraftWithProvenance(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "dev", LooseDev)

	v, err := r.Ingest(ctx, UploadSource{}, SourceRequest{Data: []byte("blob"), Opts: map[string]string{"by": "alice"}},
		"scan-tool", "tool", "Scanner", "alice", "1.0.0", nil)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if v.Status != StatusDraft {
		t.Fatalf("ingested artifact should be a draft, got %q", v.Status)
	}
	if !strings.HasPrefix(v.Ref, "sha256:") {
		t.Fatalf("draft should carry the content digest, got %q", v.Ref)
	}
	if v.Provenance["source"] != "upload" {
		t.Fatalf("draft should carry provenance, got %v", v.Provenance)
	}
}
