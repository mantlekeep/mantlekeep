package app

import "testing"

// The contract a white-label wrapper depends on: it names its OWN prefix and its own
// display values, and never types an engine variable name. These tests pin that.

func TestBrandAppliesDefaultsWithoutTheCallerNamingEngineVariables(t *testing.T) {
	t.Setenv("MANTLEKEEP_BRAND_NAME", "")
	t.Setenv("MANTLEKEEP_BRAND_MARK", "")
	t.Setenv("MANTLEKEEP_BRAND_KICKER", "")
	t.Setenv("MANTLEKEEP_BRAND_TAGLINE", "")

	Brand(BrandOptions{Prefix: "ACME", Name: "Acme Control", Mark: "◆", Tagline: "one door"})

	got := CurrentBrand()
	if got.Name != "Acme Control" || got.Mark != "◆" || got.Tagline != "one door" {
		t.Fatalf("brand defaults not applied: %+v", got)
	}
}

func TestOperatorEnvironmentWinsOverBrandDefaults(t *testing.T) {
	// An operator of the branded binary speaks only the BRAND's prefix.
	t.Setenv("ACME_BRAND_NAME", "Northwind Governance")
	t.Setenv("MANTLEKEEP_BRAND_NAME", "")

	Brand(BrandOptions{Prefix: "ACME", Name: "Acme Control"})

	if got := CurrentBrand().Name; got != "Northwind Governance" {
		t.Fatalf("operator value must win over the brand default, got %q", got)
	}
}

func TestBrandRemapsTheWholePrefixNotJustBrandKeys(t *testing.T) {
	// The remap is by prefix, so variables the framework adds LATER are covered too.
	t.Setenv("ACME_POLICY_DIR", "/etc/acme/policy")
	t.Setenv("MANTLEKEEP_POLICY_DIR", "")

	Brand(BrandOptions{Prefix: "ACME"})

	if got := get("POLICY_DIR"); got != "/etc/acme/policy" {
		t.Fatalf("prefix remap missed a non-brand variable, got %q", got)
	}
}

func TestZeroValueFieldsAreLeftAlone(t *testing.T) {
	t.Setenv("MANTLEKEEP_BRAND_NAME", "already set")

	Brand(BrandOptions{Prefix: "ACME"}) // no Name given

	if got := CurrentBrand().Name; got != "already set" {
		t.Fatalf("an empty option must not clear an existing value, got %q", got)
	}
}
