package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
)

// The diagnostic is only worth anything if it is WIRED. policy.PrecedenceNotices is tested as a
// function next to the rule it describes; these tests drive the boot paths that are supposed to
// call it and read what actually landed on stderr. A control nothing calls is worse than none.

// captureStderr runs fn with os.Stderr redirected and returns what was written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = write
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(read)
		done <- string(out)
	}()

	fn()

	os.Stderr = original
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	captured := <-done
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return captured
}

// The base cascade's boot path reports a team layer whose override a platform SEAL rejected —
// and reports the role that DECIDES, not the one the file asked for. This is the case that
// makes the diagnostic honest: the file on disk says L3-Consumer and the engine enforces
// L1-Architect, and only this line can tell an operator which.
func TestBootReportsWhenASealRejectedALayersValue(t *testing.T) {
	dir := t.TempDir()
	platform := filepath.Join(dir, "platform.json")
	team := filepath.Join(dir, "team.json")
	write := func(path, contents string) {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(platform, `{
	  "actionRoles": { "service.deploy": "L1-Architect" },
	  "sealed":      [ "action:service.deploy" ]
	}`)
	write(team, `{ "actionRoles": { "service.deploy": "L3-Consumer" } }`)
	t.Setenv("MANTLEKEEP_PLATFORM_CONFIG", platform)
	t.Setenv("MANTLEKEEP_TEAM_CONFIG", team)

	var layers []policy.Layer
	var loadErr error
	logged := captureStderr(t, func() { layers, loadErr = currentLayers(true) })
	if loadErr != nil {
		t.Fatalf("both layers are valid and must load: %v", loadErr)
	}
	if len(layers) != 3 {
		t.Fatalf("expected default+platform+team, got %d layers", len(layers))
	}
	if !strings.Contains(logged, team) {
		t.Fatalf("the boot diagnostic does not name the file it is talking about, so the "+
			"operator has a behaviour change and no place to go:\n%s", logged)
	}
	for _, must := range []string{"SEALED", "service.deploy", "L3-Consumer", "L1-Architect"} {
		if !strings.Contains(logged, must) {
			t.Fatalf("the boot diagnostic omits %q:\n%s", must, logged)
		}
	}
}

// The hot-reload poll path must stay SILENT. It re-reads the same files every couple of
// seconds; a notice there would bury the boot line it duplicates under thousands of copies.
func TestTheHotReloadPollPathEmitsNoDiagnostic(t *testing.T) {
	path := writeLayer(t, "MANTLEKEEP_TEAM_CONFIG", `{
	  "actionRoles": { "service.deploy": "L1-Architect" },
	  "sealed":      [ "action:service.deploy" ]
	}`)
	t.Setenv("MANTLEKEEP_PLATFORM_CONFIG", "")

	logged := captureStderr(t, func() {
		if _, err := currentLayers(false); err != nil {
			t.Errorf("valid layer must load: %v", err)
		}
	})
	if strings.Contains(logged, path) || strings.Contains(logged, "policy: layer") {
		t.Fatalf("the poll path narrated a precedence notice; at a two-second poll this is "+
			"the same line thousands of times:\n%s", logged)
	}
}

// A layer set that will refuse startup is passed over in silence. Notices about a law that is
// never going to run are noise in front of the error the operator actually has to fix.
func TestNoDiagnosticForALayerSetThatWillRefuseStartup(t *testing.T) {
	writeLayer(t, "MANTLEKEEP_TEAM_CONFIG", `{
	  "actionRoles": { "service.deploy": "L1-Architech" }
	}`)
	t.Setenv("MANTLEKEEP_PLATFORM_CONFIG", "")

	var layers []policy.Layer
	logged := captureStderr(t, func() { layers, _ = currentLayers(true) })
	if policy.ValidateLayers(policy.LadderFrom(layers...), layers...) == nil {
		t.Fatal("PRECONDITION: a role the ladder cannot rank must fail validation, or this " +
			"test proves nothing")
	}
	if strings.Contains(logged, "policy: layer") {
		t.Fatalf("a layer set that refuses startup was described anyway:\n%s", logged)
	}
}

// The SCOPE tier gets the diagnostic too, and it is the tier that needs it most: scope layers
// are not in the set ValidateLayers refuses startup over, so a role nobody can rank reaches the
// engine and silently refuses every subject. This line is the only warning there is.
func TestAttachScopesReportsAScopeRoleTheLadderCannotRank(t *testing.T) {
	dir := t.TempDir()
	scopePath := filepath.Join(dir, "example.json")
	if err := os.WriteFile(scopePath,
		[]byte(`{ "actionRoles": { "service.deploy": "L1-Architech" } }`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANTLEKEEP_SCOPE_CONFIG", dir)

	base := []policy.Layer{policy.DefaultLayer()}
	ladder := policy.DefaultRoleLadder()
	var engine *policy.RBAC
	logged := captureStderr(t, func() {
		engine = attachScopes(policy.NewRBAC().WithRoleLadder(ladder), base, nil, ladder, true)
	})
	if engine == nil {
		t.Fatal("attachScopes returned no engine")
	}
	for _, must := range []string{scopePath, "L1-Architech", "cannot rank"} {
		if !strings.Contains(logged, must) {
			t.Fatalf("the scope diagnostic omits %q, so the file that locks an action away "+
				"from everybody is never named:\n%s", must, logged)
		}
	}
}
