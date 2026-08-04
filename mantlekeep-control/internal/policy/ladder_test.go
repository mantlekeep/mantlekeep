package policy

import (
	"context"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// TestDefaultLadderPreservesTiers pins the built-in five-tier ranking: with NO config the engine
// ranks exactly as the old hardcoded map did (L0 senior to L1 senior to L2 …), and an unknown
// role is never senior enough. This is the default-preserving guarantee — the refactor must not
// move a single tier.
func TestDefaultLadderPreservesTiers(t *testing.T) {
	l := DefaultRoleLadder()

	// The tier chain, most senior first. Each earlier role can stand in for every later one; the
	// reverse never holds — that is the whole ordering the engine relies on.
	chain := []mantlekeep.Role{"L0-SuperAdmin", "L1-Architect", "L2-Operator", "L3-Consumer", "AI-Agent"}
	for i, senior := range chain {
		for j, junior := range chain {
			wantSeniorCovers := i <= j // a more-senior (or equal) role covers the junior floor
			if got := l.atLeastAsSenior(senior, junior); got != wantSeniorCovers {
				t.Errorf("atLeastAsSenior(%q,%q)=%v, want %v", senior, junior, got, wantSeniorCovers)
			}
			if got := l.holdsAtLeast([]string{string(senior)}, junior); got != wantSeniorCovers {
				t.Errorf("holdsAtLeast([%q], need %q)=%v, want %v", senior, junior, got, wantSeniorCovers)
			}
		}
	}

	// An unknown role is never senior enough — on either side of the comparison, and as a subject.
	if l.holdsAtLeast([]string{"L9-Ghost"}, "L1-Architect") {
		t.Error("unknown subject role must never satisfy a seniority floor")
	}
	if l.atLeastAsSenior("L9-Ghost", "L2-Operator") {
		t.Error("unknown role must never rank as senior")
	}
	if l.atLeastAsSenior("L0-SuperAdmin", "L9-Ghost") {
		t.Error("no role can stand in for an unknown required role")
	}
	// An unknown REQUIRED role is also never satisfiable (the need itself has no rank).
	if l.holdsAtLeast([]string{"L0-SuperAdmin"}, "L9-Ghost") {
		t.Error("an unknown required role has no rank and must never be satisfiable")
	}
}

// bankLadder is a deployment that renamed the built-in tiers — the exact case the old hardcoded
// map could not serve. Note "L1-Super-Admin" and "L2-Engineer" are NOT default tier names, so any
// check that still passes for them proves the CONFIGURED ladder (not the default) is in force.
func bankLadder() RoleLadder {
	return RoleLadder{
		"L0-SuperAdmin": 0, "L1-Super-Admin": 1, "L2-Engineer": 2, "L3-Consumer": 3, "AI-Agent": 4,
	}
}

// TestBankRenamedRoles_GovernEndToEnd is the demo: a bank names its OWN role ("L1-Super-Admin"),
// binds an action to it in a config layer, and the door governs on that vocabulary end-to-end.
// It drives the real RBAC.Evaluate — a full door decision, not a bare holdsAtLeast call.
func TestBankRenamedRoles_GovernEndToEnd(t *testing.T) {
	ladder := bankLadder()
	// A team layer binds the bank's action to the bank's renamed role. Resolve threads the
	// configured ladder so the binding is ranked in the bank's vocabulary.
	teamLayer := Layer{
		Name:        "team:payments",
		ActionRoles: map[string]mantlekeep.Role{"session.deploy": "L1-Super-Admin"},
	}
	resolved := Resolve(ladder, DefaultLayer(), teamLayer)
	rbac := NewRBAC().WithRoleLadder(ladder).WithResolved(resolved)

	// Helper: run one governed decision for a subject holding role, on session.deploy.
	evaluate := func(role mantlekeep.Role) mantlekeep.Decision {
		t.Helper()
		dec, err := rbac.Evaluate(context.Background(), mantlekeep.PolicyInput{
			Subject: mantlekeep.PolicySubject{ID: "kelvin", Roles: []mantlekeep.Role{role}},
			// A non-empty goal is mandatory — Evaluate denies an empty one.
			Intent: mantlekeep.PolicyIntent{Action: "session.deploy", Goal: "deploy the payments session"},
		})
		if err != nil {
			t.Fatalf("Evaluate(%q): unexpected error %v", role, err)
		}
		return dec
	}

	// ALLOW for the senior bank role — proves a renamed tier governs an action the core never
	// heard of, entirely from config.
	if dec := evaluate("L1-Super-Admin"); dec.Action != mantlekeep.ActionAllow {
		t.Errorf("L1-Super-Admin must be ALLOWED session.deploy, got %s (%s)", dec.Action, dec.Reason)
	}
	// DENY for a junior bank role — L3-Consumer is not senior enough for an L1 floor.
	if dec := evaluate("L3-Consumer"); dec.Action != mantlekeep.ActionDeny {
		t.Errorf("L3-Consumer must be DENIED session.deploy, got %s", dec.Action)
	}
	// DENY for a role absent from the bank ladder — an unknown role can never satisfy the floor.
	if dec := evaluate("L9-Ghost"); dec.Action != mantlekeep.ActionDeny {
		t.Errorf("unknown role must be DENIED session.deploy, got %s", dec.Action)
	}
}

// TestSealTighteningUsesConfiguredLadder proves the sealed-floor seniority check ranks against the
// CONFIGURED ladder, not the default. A platform layer seals session.deploy at a bank role; a team
// layer may TIGHTEN it to a more-senior bank role but may NOT loosen it. Because the bank role
// names are unknown to the default ladder, a tighten that SUCCEEDS could only have used the bank
// ladder.
func TestSealTighteningUsesConfiguredLadder(t *testing.T) {
	ladder := bankLadder()
	platform := Layer{
		Name:        "platform",
		ActionRoles: map[string]mantlekeep.Role{"session.deploy": "L2-Engineer"},
		Sealed:      []string{"action:session.deploy"},
	}

	// TIGHTEN: a team raises the floor to the more-senior L1-Super-Admin — accepted.
	tighten := Layer{Name: "team", ActionRoles: map[string]mantlekeep.Role{"session.deploy": "L1-Super-Admin"}}
	got, ok := Resolve(ladder, DefaultLayer(), platform, tighten).RequiredRole("session.deploy")
	if !ok || got != "L1-Super-Admin" {
		t.Errorf("tightening a sealed floor to a more-senior bank role must win: got %q ok=%v, want L1-Super-Admin", got, ok)
	}

	// LOOSEN: a team drops the floor to the junior L3-Consumer — rejected, sealed value kept.
	loosen := Layer{Name: "team", ActionRoles: map[string]mantlekeep.Role{"session.deploy": "L3-Consumer"}}
	got, ok = Resolve(ladder, DefaultLayer(), platform, loosen).RequiredRole("session.deploy")
	if !ok || got != "L2-Engineer" {
		t.Errorf("loosening a sealed floor must be rejected: got %q ok=%v, want the sealed L2-Engineer", got, ok)
	}
}

// TestLadderFromConfig proves the config seam: the FIRST layer that declares a non-empty Roles map
// defines the deployment vocabulary (REPLACE, not merge); no layer declaring Roles → the default.
func TestLadderFromConfig(t *testing.T) {
	// No layer declares roles → the built-in default.
	if got := LadderFrom(DefaultLayer(), Layer{Name: "team"}); len(got) != len(DefaultRoleLadder()) {
		t.Errorf("no roles declared must yield the default ladder, got %v", got)
	}

	// First non-empty wins; a later layer's roles do NOT merge in.
	base := Layer{Name: "platform", Roles: map[string]int{"L0-SuperAdmin": 0, "L1-Super-Admin": 1}}
	later := Layer{Name: "team", Roles: map[string]int{"L2-Engineer": 2}}
	got := LadderFrom(DefaultLayer(), base, later)
	if _, ok := got["L1-Super-Admin"]; !ok {
		t.Error("the first declaring layer's vocabulary must define the ladder")
	}
	if _, ok := got["L2-Engineer"]; ok {
		t.Error("a lower layer must NOT merge its roles into the ladder — first non-empty wins")
	}
}
