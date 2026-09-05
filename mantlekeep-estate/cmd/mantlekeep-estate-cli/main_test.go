package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const good = `team: payments
owns: payments
tier: dev
apps:
  - name: api
    runtime: enterprise
    image: registry.example.com/api
    placement:
      env: dev
      purpose: app
      residency: region-a
`

func TestAValidManifestParses(t *testing.T) {
	manifest, asJSON, err := readManifest(write(t, good))
	if err != nil {
		t.Fatalf("a valid manifest was refused: %v", err)
	}
	if manifest.Team != "payments" || len(manifest.Apps) != 1 {
		t.Errorf("parsed to %+v", manifest)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(asJSON)), "{") {
		t.Error("the YAML was not converted to JSON")
	}
}

// THE test. A mistyped key must be NAMED, because the name is the fix. This is the failure
// that otherwise costs a round trip to a service to discover.
func TestAMistypedKeyIsNamed(t *testing.T) {
	_, _, err := readManifest(write(t, strings.Replace(good, "residency:", "residencey:", 1)))
	if err == nil {
		t.Fatal("a manifest with an unknown field was accepted")
	}
	if !strings.Contains(err.Error(), "residencey") {
		t.Errorf("the error does not name the offending key: %v", err)
	}
}

// The other daily failure: indentation that changes meaning.
func TestABadIndentIsReportedWithItsLine(t *testing.T) {
	_, _, err := readManifest(write(t, strings.Replace(good, "      env: dev", "    env: dev", 1)))
	if err == nil {
		t.Fatal("a misindented manifest was accepted")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("the error does not say where: %v", err)
	}
}

// JSON is YAML, so a JSON manifest must work through the same path with no special case.
func TestAJSONManifestParsesToo(t *testing.T) {
	body := `{"team":"payments","owns":"payments","tier":"dev","apps":[]}`
	if _, _, err := readManifest(write(t, body)); err != nil {
		t.Fatalf("a JSON manifest was refused: %v", err)
	}
}

// KYAML is a restricted YAML dialect — flow style, always-quoted strings. It must parse with
// no special handling, which is the reason this CLI needs no KYAML-specific parser.
func TestKyamlStyleParsesAsOrdinaryYAML(t *testing.T) {
	body := `{
  "team": "payments",
  "owns": "payments",
  "tier": "dev",
  "apps": [{
    "name": "api",
    "runtime": "enterprise",
    "image": "registry.example.com/api",
    "placement": {"env": "dev", "purpose": "app", "residency": "region-a"},
  }],
}
`
	manifest, _, err := readManifest(write(t, body))
	if err != nil {
		t.Fatalf("KYAML-style input was refused: %v", err)
	}
	if manifest.Team != "payments" {
		t.Errorf("parsed to %+v", manifest)
	}
}

// A missing file says so, rather than reporting an empty manifest as valid.
func TestAMissingFileIsAnError(t *testing.T) {
	if _, _, err := readManifest("does-not-exist.yaml"); err == nil {
		t.Fatal("a missing manifest was treated as an empty one")
	}
}

// A path that walks upward is refused before it is opened.
func TestAPathThatEscapesUpwardIsRefused(t *testing.T) {
	if _, _, err := readManifest("../../../etc/passwd"); err == nil {
		t.Fatal("a traversing path was opened")
	}
}

// Flags must work on either side of the filename. Go's flag package stops at the first
// positional, so without the split `submit app.yaml -user me` would silently drop the
// identity — and this command refuses to send a change with nobody's name on it.
func TestFlagsWorkOnEitherSideOfTheFilename(t *testing.T) {
	for _, arguments := range [][]string{
		{"app.yaml", "-user", "me", "-estate", "https://e"},
		{"-user", "me", "-estate", "https://e", "app.yaml"},
		{"-user=me", "app.yaml", "-estate=https://e"},
	} {
		positional, flagArguments := splitArguments(arguments)
		if len(positional) != 1 || positional[0] != "app.yaml" {
			t.Errorf("%v → positional %v, want [app.yaml]", arguments, positional)
		}
		if len(flagArguments) != len(arguments)-1 {
			t.Errorf("%v → flags %v lost or gained an argument", arguments, flagArguments)
		}
	}
}

// A destination that is not http(s) is refused before anything is sent. A CLI that posts a
// manifest wherever it is pointed is a change looking for somewhere to go.
func TestOnlyHttpDestinationsAreAccepted(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "ftp://host", "https://", "not a url"} {
		if _, err := estateEndpoint(bad); err == nil {
			t.Errorf("%q was accepted as an estate address", bad)
		}
	}
	for _, good := range []string{"https://estate.example.com", "http://localhost:8097"} {
		if _, err := estateEndpoint(good); err != nil {
			t.Errorf("%q was refused: %v", good, err)
		}
	}
}
