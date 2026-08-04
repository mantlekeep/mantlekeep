package safeio

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCleanConfigPathRejectsTraversal proves the taint-break is real: an empty path and any
// path that walks upward via ".." are refused, while ordinary absolute/relative config paths
// pass through cleaned. This is the validation the // #nosec on the reads relies on.
func TestCleanConfigPathRejectsTraversal(t *testing.T) {
	// Rejected: paths that STILL walk upward after Clean (they escape their starting point).
	rejected := []string{
		"",
		"..",
		"../secret",
		"../../etc/passwd",
		"a/../../b", // Clean → "../b" — nets one level up, rejected
		"foo/../../..",
	}
	for _, path := range rejected {
		if got, err := CleanConfigPath(path); err == nil {
			t.Errorf("CleanConfigPath(%q) = %q, want error (upward traversal must be rejected)", path, got)
		}
	}

	// Accepted: Clean resolves any interior ".." without the result escaping upward, so the
	// path names a real, in-bounds location (absolute paths can never escape their root).
	accepted := map[string]string{
		"config.json":                 "config.json",
		"./config.json":               "config.json",
		"/etc/mantlekeep/grants":      "/etc/mantlekeep/grants",
		"a/b/c.json":                  "a/b/c.json",
		"a/./b.json":                  "a/b.json",          // Clean collapses the no-op element without escaping
		"config/..":                   ".",                 // resolves to the working dir, not an escape
		"/etc/../../root/.ssh/id_rsa": "/root/.ssh/id_rsa", // absolute: ".." cannot climb past root
	}
	for in, want := range accepted {
		got, err := CleanConfigPath(in)
		if err != nil {
			t.Errorf("CleanConfigPath(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("CleanConfigPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReadConfigFileRoundTrip proves the validated read returns the same bytes as a bare read
// for a legitimate path, and refuses a traversal path before touching the filesystem.
func TestReadConfigFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layer.json")
	want := []byte(`{"roles":{}}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile(%q): %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("ReadConfigFile = %q, want %q", got, want)
	}

	if _, err := ReadConfigFile("../escape.json"); err == nil {
		t.Error("ReadConfigFile accepted an upward-traversal path, want rejection")
	}
}
