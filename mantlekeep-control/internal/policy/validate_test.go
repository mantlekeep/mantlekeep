package policy

import (
	"strings"
	"testing"

	mantlekeep "mantlekeep.dev/control"
)

// TestValidateLayersRejectsUndefinedRole proves the fail-closed semantic cross-check: a layer
// that binds an action to a role the ladder never defined (the classic misspelling) is a config
// ERROR, and the error names the offending action and role so an operator fixes the exact typo.
func TestValidateLayersRejectsUndefinedRole(t *testing.T) {
	ladder := RoleLadder{"L1-Super-Admin": 1, "L2-Engineer": 2}
	layer := Layer{
		Name:        "team:example",
		ActionRoles: map[string]mantlekeep.Role{"session.deploy": "L1-Super-Admn"}, // typo
	}

	err := ValidateLayers(ladder, layer)
	if err == nil {
		t.Fatal("a role not in the ladder must be a config error (fail closed)")
	}
	if !strings.Contains(err.Error(), "session.deploy") || !strings.Contains(err.Error(), "L1-Super-Admn") {
		t.Errorf("error must name the offending action AND role, got: %v", err)
	}
}

// TestValidateLayersAcceptsDefinedRoles proves a well-formed layer whose roles all exist in the
// ladder validates clean — the guard must not reject a legitimate config.
func TestValidateLayersAcceptsDefinedRoles(t *testing.T) {
	ladder := RoleLadder{"L1-Super-Admin": 1, "L2-Engineer": 2}
	layer := Layer{
		Name:        "team:example",
		ActionRoles: map[string]mantlekeep.Role{"session.deploy": "L1-Super-Admin"},
		Sealed:      []string{"action:session.deploy"},
	}
	if err := ValidateLayers(ladder, layer); err != nil {
		t.Fatalf("a layer whose roles are all defined must validate: %v", err)
	}
}

// TestValidateLayersDefaultsWhenNoRolesDeclared proves that when no layer declares a `roles`
// map, the built-in default ladder is used — so a config referencing a built-in tier name is
// valid without an explicit vocabulary.
func TestValidateLayersDefaultsWhenNoRolesDeclared(t *testing.T) {
	layer := Layer{
		Name:        "team:example",
		ActionRoles: map[string]mantlekeep.Role{"session.deploy": mantlekeep.RoleArchitect},
	}
	// A nil ladder means "use the default" — RoleArchitect (L1-Architect) is a built-in tier.
	if err := ValidateLayers(nil, layer); err != nil {
		t.Fatalf("a built-in role under the default ladder must validate: %v", err)
	}
	// And a role NOT in the default ladder still fails closed under defaults.
	bad := Layer{Name: "team:example", ActionRoles: map[string]mantlekeep.Role{"x": "Nonexistent"}}
	if err := ValidateLayers(nil, bad); err == nil {
		t.Fatal("an undefined role must fail closed even under the default ladder")
	}
}

// TestValidateLayersRejectsMalformedSeal proves a seal that is not a well-formed "action:<name>"
// reference (a bare action, or a role mistakenly placed in `sealed`) is a config error rather
// than a silent no-op floor that protects nothing.
func TestValidateLayersRejectsMalformedSeal(t *testing.T) {
	ladder := DefaultRoleLadder()
	for _, bad := range []string{"session.deploy", "action:", "L1-Architect"} {
		layer := Layer{Name: "team:example", Sealed: []string{bad}}
		if err := ValidateLayers(ladder, layer); err == nil {
			t.Errorf("malformed seal %q must be a config error", bad)
		}
	}
}
