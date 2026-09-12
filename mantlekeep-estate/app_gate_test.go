package estate

import "testing"

// A named app may cost MORE human attention than its tier demands, and never less.
//
// # Why this exists
//
// Gates were keyed by tier alone, and GateFor says so: "the ONLY place tier becomes a gate, so the
// answer cannot differ between assets — a prod Kafka topic and a prod database cost the same
// attention because the blast radius, not the technology, is what is being governed."
//
// That rule is right about ASSETS and silent about APPS. A bank has individual applications that
// carry consequence their tier does not describe — a payments engine in a shared environment, a
// system under a regulator's specific attention — and the platform team needs to say "this one
// always waits for a person" without moving the whole tier.
//
// # The direction is the whole design
//
// An app rule may only RAISE. The same seam as config gates: "config chooses the policy and may
// raise a gate, but it cannot lower the floor." A whitelist that EXEMPTED an app from approval
// would be the bypass that makes a guardrail govern nothing — and it would be the single most
// attractive line in the file to anyone wanting their change through.
//
// Enforced at RESOLUTION, not only at config load, because a Floor can be built in code and never
// pass through the config validator.

func TestAnAppRuleRaisesTheGate(t *testing.T) {
	floor := DefaultFloor()
	// dev is ungated by default; this app is not.
	floor.Apps = map[string]AppRule{"payments/settlement-engine": {Gate: GatePlatform}}

	if got := floor.GateForApp(TierDev, "payments/settlement-engine"); got != GatePlatform {
		t.Errorf("a named app must take its own stronger gate; got %q, want %q", got, GatePlatform)
	}
}

// A weaker rule changes nothing. This is the sealed floor, stated as a test.
func TestAnAppRuleCanNeverLowerTheGate(t *testing.T) {
	floor := DefaultFloor()
	// prod is GatePlatform by default. Ask for none, and for the weaker owning-team.
	for _, weaker := range []Gate{GateNone, GateOwningTeam} {
		floor.Apps = map[string]AppRule{"payments/checkout": {Gate: weaker}}

		got := floor.GateForApp(TierProd, "payments/checkout")
		if got != GatePlatform {
			t.Errorf("an app rule of %q lowered a prod gate to %q — an app that can exempt "+
				"itself from approval is the bypass that makes the gate govern nothing",
				weaker, got)
		}
	}
}

// An app nobody named is governed exactly as its tier says.
func TestAnUnnamedAppIsUnaffected(t *testing.T) {
	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{"payments/settlement-engine": {Gate: GatePlatform}}

	for _, tier := range []Tier{TierDev, TierShared, TierProd} {
		want := floor.GateFor(tier)
		if got := floor.GateForApp(tier, "treasury/reporting"); got != want {
			t.Errorf("tier %q: an unnamed app must be governed by its tier alone; got %q want %q",
				tier, got, want)
		}
	}
}

// A gate this build does not know is not treated as permissive.
//
// The same reasoning as validateGates refusing one: "an unrecognised gate is refused rather than
// assumed permissive". Here, at resolution, the safe answer is the tier's own gate — never the
// unknown value, which no adapter could honour and which Strength() ranks at zero.
func TestAnUnknownGateInAnAppRuleIsIgnored(t *testing.T) {
	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{"payments/checkout": {Gate: Gate("rubber-stamp")}}

	if got := floor.GateForApp(TierProd, "payments/checkout"); got != GatePlatform {
		t.Errorf("an unrecognised gate must not weaken a tier; got %q", got)
	}
}

// An empty rule is not a rule. A key with no gate must not read as "no gate".
func TestAnAppRuleWithNoGateChangesNothing(t *testing.T) {
	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{"payments/checkout": {}}

	if got := floor.GateForApp(TierProd, "payments/checkout"); got != GatePlatform {
		t.Errorf("a rule that names no gate must leave the tier's gate alone; got %q", got)
	}
}

// The key is TEAM-QUALIFIED, so one team's app cannot gate another's.
//
// App names are not unique across a bank — two teams both have a "checkout". A bare app name in
// this table would gate every team's, which is a rule nobody wrote reaching a team nobody told.
func TestTheAppKeyIsTeamQualified(t *testing.T) {
	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{"payments/checkout": {Gate: GatePlatform}}

	if got := floor.GateForApp(TierDev, "treasury/checkout"); got != GateNone {
		t.Errorf("another team's app of the same name was gated by this rule; got %q", got)
	}
	if got := floor.GateForApp(TierDev, "checkout"); got != GateNone {
		t.Errorf("an unqualified name matched a qualified rule; got %q", got)
	}
}

// The rule must reach a RESOLVED change, or it governs nothing.
//
// A floor field nothing reads is worse than an absent one: an operator writes the rule, the file
// loads, the boot log names it, and every deployment goes through ungated — positive feedback for
// a control that does not exist.
func TestAnAppRuleReachesTheResolvedChange(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"settlement-engine","runtime":"enterprise","image":"h/p/se",
	             "placement":{"env":"dev","purpose":"app","residency":"uk"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	clusters := NewPlacer([]Cluster{{Name: "dev-app-uk-1", Env: "dev", Purpose: "app",
		Residency: "uk", Reachable: true}})

	// Baseline: dev is ungated, so this app applies immediately.
	plain, err := ResolveWith(manifest, DefaultFloor(), clusters, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if gate := appGateIn(t, plain); gate != GateNone {
		t.Fatalf("a dev app must be ungated by default; got %q", gate)
	}

	// Now the platform names it. Same manifest, same tier, same team.
	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{"payments/settlement-engine": {Gate: GatePlatform}}

	gated, err := ResolveWith(manifest, floor, clusters, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if gate := appGateIn(t, gated); gate != GatePlatform {
		t.Errorf("the app rule did not reach the resolved change: gate is %q, want %q — the "+
			"deployment would apply with nobody asked", gate, GatePlatform)
	}
}

// appGateIn returns the gate on the one app change in a resolved estate.
func appGateIn(t *testing.T, desired Desired) Gate {
	t.Helper()
	for _, change := range desired.Changes {
		if change.Asset == "app" {
			return change.Gate
		}
	}
	t.Fatal("no app change was resolved, so the gate could not be checked")
	return ""
}
