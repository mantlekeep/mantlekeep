package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Generic capability selection: MANTLEKEEP_SELECT_<CAP>.
func TestSelectCapability(t *testing.T) {
	t.Setenv("MANTLEKEEP_SELECT_FORGE", "github")
	t.Setenv("MANTLEKEEP_SELECT_POLICY", "cedar")
	c := Load()
	if c.Select("forge") != "github" || c.Select("policy") != "cedar" {
		t.Fatalf("selections not read: %v", c.Selections)
	}
	if c.Select("unset") != "" {
		t.Fatal("unset capability should be empty")
	}
}

// Generic role binding: MANTLEKEEP_BIND_<CAP>_<ROLE>, overlaid on defaults.
func TestBindRoutesOverrideDefaults(t *testing.T) {
	t.Setenv("MANTLEKEEP_BIND_STORE_CACHE", "postgres")
	routes := Load().Routes("store", map[string]string{
		"audit": "postgres", "events": "mysql", "cache": "mariadb",
	})
	if routes["cache"] != "postgres" {
		t.Fatalf("cache should be overridden, got %q", routes["cache"])
	}
	if routes["audit"] != "postgres" || routes["events"] != "mysql" {
		t.Fatalf("unset roles must keep defaults, got %v", routes)
	}
}

// The same generic binding works for ANY capability, not just store.
func TestBindGenericAcrossCapabilities(t *testing.T) {
	t.Setenv("MANTLEKEEP_BIND_AUDIT_LONGTERM", "s3")
	routes := Load().Routes("audit", map[string]string{"longterm": "bbolt"})
	if routes["longterm"] != "s3" {
		t.Fatalf("audit binding should apply generically, got %q", routes["longterm"])
	}
}

// External drivers: MANTLEKEEP_DRIVER_<NAME>.
func TestDriverBinaries(t *testing.T) {
	t.Setenv("MANTLEKEEP_DRIVER_POSTGRES", "/opt/mantlekeep/driver-postgres")
	if Load().Drivers["postgres"] != "/opt/mantlekeep/driver-postgres" {
		t.Fatal("MANTLEKEEP_DRIVER_POSTGRES not read")
	}
}

// A .mantlekeep.conf file supplies config; environment variables override it.
func TestConfigFileWithEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mantlekeep.conf")
	if err := os.WriteFile(path, []byte(
		"# comment\nMANTLEKEEP_SELECT_FORGE=forgejo\nMANTLEKEEP_BIND_STORE_AUDIT=postgres\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANTLEKEEP_CONFIG", path)
	// env overrides the file's forge selection.
	t.Setenv("MANTLEKEEP_SELECT_FORGE", "github")

	c := Load()
	if c.Select("forge") != "github" {
		t.Fatalf("env should override file, got %q", c.Select("forge"))
	}
	if c.Routes("store", nil)["audit"] != "postgres" {
		t.Fatalf("file binding should be read, got %v", c.Bindings)
	}
}
