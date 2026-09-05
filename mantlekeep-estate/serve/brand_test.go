package serve

import "testing"

// A deployment that rebrands reads its OWN variable names. The names appear in manifests,
// Helm values and runbooks, so a product name baked into them would make rebranding a
// migration rather than a setting.
func TestARebrandedDeploymentReadsItsOwnNames(t *testing.T) {
	t.Setenv(BrandPrefixVar, "ACME")
	t.Setenv("ACME_ESTATE_CONFIG", "/acme/floor.json")

	if got := envOr("MANTLEKEEP_ESTATE_CONFIG", ""); got != "/acme/floor.json" {
		t.Errorf("envOr = %q, want the branded value", got)
	}
}

// A deployment that has NOT rebranded keeps working with no change at all.
func TestTheDefaultNamesKeepWorking(t *testing.T) {
	t.Setenv("MANTLEKEEP_ESTATE_CONFIG", "/default/floor.json")
	if got := envOr("MANTLEKEEP_ESTATE_CONFIG", ""); got != "/default/floor.json" {
		t.Errorf("envOr = %q, want the default value", got)
	}
}

// Mid-migration: a brand is set but only some variables have moved. The unmoved ones must
// still resolve, or rebranding would be an all-at-once edit.
func TestAPartialMigrationStillResolves(t *testing.T) {
	t.Setenv(BrandPrefixVar, "ACME")
	t.Setenv("MANTLEKEEP_ESTATE_FLEET", "/not/yet/moved.json")

	if got := envOr("MANTLEKEEP_ESTATE_FLEET", ""); got != "/not/yet/moved.json" {
		t.Errorf("envOr = %q — an unmoved variable stopped resolving mid-migration", got)
	}
}

// The branded name WINS when both are set, or a migration could never finish.
func TestTheBrandedNameWins(t *testing.T) {
	t.Setenv(BrandPrefixVar, "ACME")
	t.Setenv("ACME_ESTATE_CONFIG", "/acme/floor.json")
	t.Setenv("MANTLEKEEP_ESTATE_CONFIG", "/old/floor.json")

	if got := envOr("MANTLEKEEP_ESTATE_CONFIG", ""); got != "/acme/floor.json" {
		t.Errorf("envOr = %q, want the branded value to win", got)
	}
}

// A lowercase brand still resolves: an operator writing "acme" means ACME_*.
func TestALowercaseBrandResolves(t *testing.T) {
	t.Setenv(BrandPrefixVar, "acme")
	t.Setenv("ACME_ESTATE_KSM", "http://ksm")
	if got := envOr("MANTLEKEEP_ESTATE_KSM", ""); got != "http://ksm" {
		t.Errorf("envOr = %q — a lowercase brand did not resolve", got)
	}
}
