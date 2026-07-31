package policy

import "testing"

// The namespace prefixes the PolicyID on every audit record — a value that cannot be
// changed afterwards, since a decision already on the chain cannot be relabelled. So a
// white-labelled deployment must be able to set it BEFORE it starts recording.

func TestDefaultsToTheFrameworksOwnName(t *testing.T) {
	t.Setenv(NamespaceEnv, "")
	t.Setenv(brandEnv, "")

	if got := policyID("rbac"); got != "mantlekeep.rbac" {
		t.Fatalf("an unconfigured deployment should use the framework's name, got %q", got)
	}
}

func TestAnExplicitNamespaceWins(t *testing.T) {
	t.Setenv(NamespaceEnv, "acme")
	t.Setenv(brandEnv, "Something Else")

	if got := policyID("rbac"); got != "acme.rbac" {
		t.Fatalf("the explicit setting must win over the brand, got %q", got)
	}
}

func TestTheBrandIsUsedWhenNoNamespaceIsSet(t *testing.T) {
	// A product that already declared its brand should not have to say the same name twice.
	t.Setenv(NamespaceEnv, "")
	t.Setenv(brandEnv, "Acme Platform")

	if got := policyID("rbac"); got != "acme-platform.rbac" {
		t.Fatalf("a display name should reduce to an identifier, got %q", got)
	}
}

func TestFailsafeCarriesTheSameNamespace(t *testing.T) {
	// Both kinds must move together: a ledger where one decision says acme.rbac and
	// another says mantlekeep.failsafe tells an auditor two different stories.
	t.Setenv(NamespaceEnv, "acme")

	if got := policyID("failsafe"); got != "acme.failsafe" {
		t.Fatalf("got %q", got)
	}
}

func TestADisplayNameNeverProducesANamelessPolicy(t *testing.T) {
	t.Setenv(NamespaceEnv, "")
	t.Setenv(brandEnv, "!!!") // nothing usable

	if got := policyID("rbac"); got != "mantlekeep.rbac" {
		t.Fatalf("an unusable brand must fall back, not produce a nameless policy: %q", got)
	}
}
