package grants

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These pin the LAYERING that Load() performs: the embedded (empty) baseline, then the
// IT-owned PLATFORM doc which is exempt from the seal and defines it, then the PRODUCT
// docs which are subject to it. The seal is the governance claim in this file — a
// product may never grant a verb the platform owns — so most of what follows is about
// where that refusal comes from.

func writeDoc(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The core binary embeds NO policy: with nothing configured, Load yields a well-formed
// zero value. No grants means every action is denied — fail closed.
func TestLoadWithNothingConfiguredYieldsNoGrants(t *testing.T) {
	t.Setenv(EnvOverride, "")
	t.Setenv(PlatformPolicyEnv, "")
	t.Setenv(PolicyDirEnv, "")

	g, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.RoleActions == nil {
		t.Error("RoleActions is nil — a caller would panic writing to it")
	}
	if len(g.RoleActions) != 0 {
		t.Errorf("the core shipped grants of its own: %v", g.RoleActions)
	}
	if len(g.ApprovalActions) != 0 {
		t.Errorf("the core shipped approval actions of its own: %v", g.ApprovalActions)
	}
}

// A document that omits role_actions entirely unmarshals it as nil, and the platform
// merge writes into that map — assigning to a nil map panics. The guard in Load is what
// stops an override as ordinary as "{}" from taking the process down.
func TestAnOverrideOmittingRoleActionsStillMergesThePlatformLayer(t *testing.T) {
	t.Setenv(EnvOverride, `{}`)
	t.Setenv(PolicyDirEnv, "")
	t.Setenv(PlatformPolicyEnv, `{"role_actions": {"L1-Architect": ["policy.change"]}}`)

	g, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.RoleActions == nil {
		t.Fatal("RoleActions is nil — the next write to it panics")
	}
	if got := g.RoleActions["L1-Architect"]; len(got) != 1 || got[0] != "policy.change" {
		t.Errorf("platform grants did not merge onto an override with no role_actions: %v", g.RoleActions)
	}
}

func TestLoadAcceptsTheOverrideAsInlineJSONOrAsAPath(t *testing.T) {
	t.Setenv(PlatformPolicyEnv, "")
	t.Setenv(PolicyDirEnv, "")
	doc := `{"role_actions":{"L2-Operator":["job.run"]},"approval_actions":["change.approve"]}`

	t.Run("inline", func(t *testing.T) {
		t.Setenv(EnvOverride, doc)
		g, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := g.RoleActions["L2-Operator"]; len(got) != 1 || got[0] != "job.run" {
			t.Errorf("inline override not applied: %v", g.RoleActions)
		}
	})

	t.Run("file path", func(t *testing.T) {
		t.Setenv(EnvOverride, writeDoc(t, t.TempDir(), "grants.json", doc))
		g, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := g.RoleActions["L2-Operator"]; len(got) != 1 || got[0] != "job.run" {
			t.Errorf("file override not applied: %v", g.RoleActions)
		}
	})
}

func TestLoadFailsOnAMalformedDocument(t *testing.T) {
	t.Setenv(PlatformPolicyEnv, "")
	t.Setenv(PolicyDirEnv, "")
	t.Setenv(EnvOverride, `{"role_actions": NOT JSON`)

	if _, err := Load(); err == nil {
		t.Fatal("a malformed grant document loaded without error — it must fail fast")
	}
}

func TestPlatformGrantsMergeInAndProductsInheritThem(t *testing.T) {
	t.Setenv(EnvOverride, "")
	t.Setenv(PolicyDirEnv, "")
	t.Setenv(PlatformPolicyEnv, `{
		"role_actions": {"L1-Architect": ["policy.change"]},
		"approval_actions": ["policy.change"]
	}`)

	g, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := g.RoleActions["L1-Architect"]; len(got) != 1 || got[0] != "policy.change" {
		t.Errorf("platform grants did not merge: %v", g.RoleActions)
	}
	if len(g.ApprovalActions) != 1 || g.ApprovalActions[0] != "policy.change" {
		t.Errorf("platform approval actions did not merge: %v", g.ApprovalActions)
	}
}

// The seal must not depend on IT remembering a list: every verb the platform GRANTS is
// sealed automatically, even when sealed_actions never names it.
func TestAProductMayNotGrantAVerbThePlatformGrants(t *testing.T) {
	t.Setenv(EnvOverride, "")
	t.Setenv(PlatformPolicyEnv, `{"role_actions": {"L1-Architect": ["policy.change"]}}`)

	dir := t.TempDir()
	writeDoc(t, dir, "product.json", `{"role_actions": {"L3-Consumer": ["policy.change"]}}`)
	t.Setenv(PolicyDirEnv, dir)

	_, err := Load()
	if err == nil {
		t.Fatal("a product granted a platform verb and the load succeeded — the seal is open")
	}
	for _, want := range []string{"policy.change", "L3-Consumer", "sealed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, so an operator cannot act on it: %v", want, err)
		}
	}
}

// sealed_actions seals verbs the platform forbids products from granting even though the
// platform itself does not grant them.
func TestSealedActionsSealVerbsThePlatformDoesNotGrant(t *testing.T) {
	t.Setenv(EnvOverride, "")
	t.Setenv(PlatformPolicyEnv, `{"sealed_actions": ["cluster.destroy"]}`)

	dir := t.TempDir()
	writeDoc(t, dir, "product.json", `{"role_actions": {"L3-Consumer": ["cluster.destroy"]}}`)
	t.Setenv(PolicyDirEnv, dir)

	if _, err := Load(); err == nil {
		t.Fatal("a product granted an explicitly sealed verb and the load succeeded")
	}
}

func TestAProductMayGrantItsOwnActions(t *testing.T) {
	t.Setenv(EnvOverride, "")
	t.Setenv(PlatformPolicyEnv, `{"role_actions": {"L1-Architect": ["policy.change"]}}`)

	dir := t.TempDir()
	writeDoc(t, dir, "product.json", `{
		"role_actions": {"L3-Consumer": ["sdlc.build"]},
		"approval_actions": ["sdlc.release"]
	}`)
	t.Setenv(PolicyDirEnv, dir)

	g, err := Load()
	if err != nil {
		t.Fatalf("a product granting its OWN action was refused: %v", err)
	}
	if got := g.RoleActions["L3-Consumer"]; len(got) != 1 || got[0] != "sdlc.build" {
		t.Errorf("product grants did not merge: %v", g.RoleActions)
	}
	if len(g.ApprovalActions) != 1 || g.ApprovalActions[0] != "sdlc.release" {
		t.Errorf("product approval actions did not merge: %v", g.ApprovalActions)
	}
	// The platform layer is still there underneath.
	if got := g.RoleActions["L1-Architect"]; len(got) != 1 {
		t.Errorf("the platform layer was lost: %v", g.RoleActions)
	}
}

// A merge must be reproducible, so docs are read in sorted order; only *.json in a
// directory counts, and a plain file path is taken as one doc.
func TestProductDocsAreSortedJSONFilesFromFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "b.json", `{"role_actions": {"r": ["b"]}}`)
	writeDoc(t, dir, "a.json", `{"role_actions": {"r": ["a"]}}`)
	writeDoc(t, dir, "notes.txt", `ignore me`)

	loose := writeDoc(t, t.TempDir(), "loose.json", `{"role_actions": {"r": ["c"]}}`)
	t.Setenv(PolicyDirEnv, dir+string(os.PathListSeparator)+loose)

	docs, err := productDocs()
	if err != nil {
		t.Fatalf("productDocs: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("read %d docs, want 3 (a.json, b.json, loose.json — notes.txt is not one)", len(docs))
	}
	// a.json sorts before b.json; the loose path's temp dir sorts independently, so assert
	// only the ordering that is guaranteed within the directory.
	if !strings.HasSuffix(docs[0].Source, "a.json") || !strings.HasSuffix(docs[1].Source, "b.json") {
		t.Errorf("docs are not in sorted order: %s, %s", docs[0].Source, docs[1].Source)
	}
}

func TestProductDocsFailsLoudlyOnAMissingOrMalformedDoc(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		t.Setenv(PolicyDirEnv, filepath.Join(t.TempDir(), "nope.json"))
		if _, err := productDocs(); err == nil {
			t.Fatal("a missing policy source loaded without error")
		}
	})

	t.Run("malformed doc", func(t *testing.T) {
		dir := t.TempDir()
		writeDoc(t, dir, "bad.json", `{"role_actions": OOPS}`)
		t.Setenv(PolicyDirEnv, dir)
		if _, err := productDocs(); err == nil {
			t.Fatal("a malformed policy doc loaded without error")
		}
	})
}

func TestProductDocsIsEmptyWhenUnset(t *testing.T) {
	t.Setenv(PolicyDirEnv, "")
	docs, err := productDocs()
	if err != nil {
		t.Fatalf("productDocs: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("read %d docs with nothing configured, want 0", len(docs))
	}
}
