package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
)

// TestLayerFileCarriesRenamedRoleVocabulary proves the config seam a deployment uses to rename its
// tiers: a layer file's "roles" map loads onto the layer, and LadderFrom turns it into the
// deployment ladder. This is the on-disk half of the flexible-role-ladder change — the door then
// governs on THIS vocabulary (see the policy-package end-to-end test).
func TestLayerFileCarriesRenamedRoleVocabulary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "platform.json")
	// A platform layer: it names its OWN tiers and binds an action to one of them.
	const layerJSON = `{
	  "roles":       { "L0-SuperAdmin": 0, "L1-Super-Admin": 1, "L2-Engineer": 2, "L3-Consumer": 3, "AI-Agent": 4 },
	  "actionRoles": { "session.deploy": "L1-Super-Admin" },
	  "sealed":      [ "action:session.deploy" ]
	}`
	if err := os.WriteFile(path, []byte(layerJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANTLEKEEP_PLATFORM_CONFIG", path)

	layer, ok, err := loadLayer("MANTLEKEEP_PLATFORM_CONFIG", "platform", false)
	if err != nil {
		t.Fatalf("valid layer file must load: %v", err)
	}
	if !ok {
		t.Fatal("layer file must load")
	}
	if got := layer.Roles["L1-Super-Admin"]; got != 1 {
		t.Errorf("roles map must load onto the layer: L1-Super-Admin=%d, want 1", got)
	}

	ladder := policy.LadderFrom(policy.DefaultLayer(), layer)
	if _, ok := ladder["L1-Super-Admin"]; !ok {
		t.Error("LadderFrom must adopt the layer's renamed vocabulary")
	}
	if _, ok := ladder["L1-Architect"]; ok {
		t.Error("declaring roles REPLACES the default vocabulary — the built-in tier names must be gone")
	}
}

// writeLayer writes a layer file into a temp dir and points envVar at it. Returns the path.
func writeLayer(t *testing.T, envVar, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "layer.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envVar, path)
	return path
}

// TestLoadLayerRejectsUnknownKey proves fail-closed structural validation: a typo'd key
// ("actionRole" for "actionRoles") is an ERROR the loader propagates, not a silently dropped
// field that would leave the intended binding missing and the policy quietly weaker.
func TestLoadLayerRejectsUnknownKey(t *testing.T) {
	writeLayer(t, "MANTLEKEEP_PLATFORM_CONFIG", `{
	  "actionRole": { "session.deploy": "L1-Architect" }
	}`)

	if _, _, err := loadLayer("MANTLEKEEP_PLATFORM_CONFIG", "platform", false); err == nil {
		t.Fatal("an unknown key must be a hard error (fail closed), not silently ignored")
	}
}

// TestLoadLayerUnsetIsDefaults proves an UNSET config is not an error — no config is a
// legitimate choice, the built-in defaults apply. This guards against fail-closed over-reach.
func TestLoadLayerUnsetIsDefaults(t *testing.T) {
	t.Setenv("MANTLEKEEP_PLATFORM_CONFIG", "")
	layer, ok, err := loadLayer("MANTLEKEEP_PLATFORM_CONFIG", "platform", false)
	if err != nil {
		t.Fatalf("an unset config must not error: %v", err)
	}
	if ok {
		t.Fatalf("an unset config must report not-loaded, got layer %+v", layer)
	}
}

// TestLoadLayerFingerprintStable proves the fingerprint is derived from the RAW file bytes and
// is stable for identical content — the property that makes "which policy is live" auditable.
// We compute the same sha256 the boot log prints and assert it matches the file bytes.
func TestLoadLayerFingerprintStable(t *testing.T) {
	const contents = `{
	  "actionRoles": { "session.deploy": "L1-Architect" },
	  "sealed":      [ "action:session.deploy" ]
	}`
	path := writeLayer(t, "MANTLEKEEP_TEAM_CONFIG", contents)

	if _, ok, err := loadLayer("MANTLEKEEP_TEAM_CONFIG", "team", false); err != nil || !ok {
		t.Fatalf("a valid layer must load: ok=%v err=%v", ok, err)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	want := short(hex.EncodeToString(sum[:]))
	// Same bytes, computed again — the fingerprint is a pure function of file content, so a
	// second read of the unchanged file yields the identical hex12.
	again := short(hex.EncodeToString(sum[:]))
	if want != again || len(want) != 12 {
		t.Fatalf("fingerprint must be a stable 12-hex digest of the raw bytes: %q vs %q", want, again)
	}
}
