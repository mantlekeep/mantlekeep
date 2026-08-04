package app

import (
	"os"
	"path/filepath"
	"testing"

	"mantlekeep.dev/control/internal/policy"
)

// TestLayerFileCarriesRenamedRoleVocabulary proves the config seam a bank uses to rename its
// tiers: a layer file's "roles" map loads onto the layer, and LadderFrom turns it into the
// deployment ladder. This is the on-disk half of the flexible-role-ladder change — the door then
// governs on THIS vocabulary (see the policy-package end-to-end test).
func TestLayerFileCarriesRenamedRoleVocabulary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "platform.json")
	// A bank platform layer: it names its OWN tiers and binds an action to one of them.
	const layerJSON = `{
	  "roles":       { "L0-SuperAdmin": 0, "L1-Super-Admin": 1, "L2-Engineer": 2, "L3-Consumer": 3, "AI-Agent": 4 },
	  "actionRoles": { "session.deploy": "L1-Super-Admin" },
	  "sealed":      [ "action:session.deploy" ]
	}`
	if err := os.WriteFile(path, []byte(layerJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANTLEKEEP_PLATFORM_CONFIG", path)

	layer, ok := loadLayer("MANTLEKEEP_PLATFORM_CONFIG", "platform", false)
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
