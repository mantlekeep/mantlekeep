package estate

import (
	"strings"
	"testing"
)

// A floor may PREFER clusters for a named app, in order, and can never make an illegal one legal.
//
// # Why this exists
//
// The team never names a cluster — it declares env, purpose and residency, and the platform
// chooses. That is the design and it stays. But a platform team has reasons a claim cannot
// express: an app that must sit beside a system it talks to, one a regulator has asked to be
// isolated, one whose licence is bought per cluster.
//
// So the preference is the PLATFORM's, read from the floor and keyed by app. It is deliberately
// NOT a field on Placement, because Placement is parsed from the team's own manifest — a team that
// could assert a preferred cluster would be choosing its own placement, which is the thing the
// design removed.
//
// # Where it sits in Place, and why that is the whole design
//
// Place is three ordered steps and says so: "legality, then stickiness, then capacity. The order
// IS the guarantee." A preference is inserted between stickiness and capacity:
//
//	legality      — unchanged and FIRST. A preference cannot reach it, so no pin can put data in
//	                the wrong jurisdiction or send it to a cluster nobody can see.
//	stickiness    — unchanged and still ahead. A running app is not migrated because somebody
//	                edited a document; moving a placed app is a new governed decision.
//	PREFERENCE    — new. Among clusters already legal, try the named ones in order.
//	capacity      — the tiebreak, and the fallback when no preferred cluster can take the work.

// Named because each is asserted in several cases here, and copies of a format string are copies
// to edit when the wording changes.
const (
	litPlaceFailed   = "place: %v"
	litResolveFailed = "resolve: %v"
)

func clustersForTest() []Cluster {
	return []Cluster{
		{Name: "uk-app-1", Env: "prod", Purpose: "app", Residency: "uk", Reachable: true},
		{Name: "uk-app-2", Env: "prod", Purpose: "app", Residency: "uk", Reachable: true},
		{Name: "uk-app-3", Env: "prod", Purpose: "app", Residency: "uk", Reachable: true},
		// Legal for nothing this file claims: another jurisdiction entirely.
		{Name: "sg-app-1", Env: "prod", Purpose: "app", Residency: "sg", Reachable: true},
		// Named, in-jurisdiction, and unreachable — we cannot see it, so we cannot place into it.
		{Name: "uk-app-dark", Env: "prod", Purpose: "app", Residency: "uk", Reachable: false},
	}
}

func ukClaim() Placement {
	return Placement{Env: "prod", Purpose: "app", Residency: "uk"}
}

// A preferred cluster is chosen over the one capacity would have picked.
func TestAPreferredClusterWinsOverTheEmptiest(t *testing.T) {
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.20},
		{Cluster: "uk-app-2", AllocatablePct: 0.90}, // capacity alone would choose this
		{Cluster: "uk-app-3", AllocatablePct: 0.50},
	})

	decision, err := placer.PlaceWith(ukClaim(), "", []string{"uk-app-1"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster != "uk-app-1" {
		t.Errorf("the preferred cluster was not chosen; got %q, want uk-app-1", decision.Cluster)
	}
}

// Preferences are tried IN ORDER: plan A, then plan B.
func TestAPreferenceFallsThroughToTheNextWhenTheFirstIsFull(t *testing.T) {
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.02}, // below minFree: pods would never schedule
		{Cluster: "uk-app-2", AllocatablePct: 0.40},
		{Cluster: "uk-app-3", AllocatablePct: 0.90},
	})

	decision, err := placer.PlaceWith(ukClaim(), "", []string{"uk-app-1", "uk-app-2"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster != "uk-app-2" {
		t.Errorf("plan B was not used when plan A was full; got %q, want uk-app-2", decision.Cluster)
	}
}

// A preference can never make an ILLEGAL cluster legal. This is the one that matters.
func TestAPreferenceCannotReachAnIllegalCluster(t *testing.T) {
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "sg-app-1", AllocatablePct: 0.99},
		{Cluster: "uk-app-1", AllocatablePct: 0.30},
	})

	// The floor asks for the Singapore cluster for a UK-resident claim.
	decision, err := placer.PlaceWith(ukClaim(), "", []string{"sg-app-1"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster == "sg-app-1" {
		t.Fatal("a preference placed UK-resident data in a Singapore cluster — legality must " +
			"filter BEFORE preference, or a pin becomes the way out of residency")
	}
}

// An unreachable cluster is not chosen however hard a floor prefers it.
func TestAPreferenceCannotReachAnUnseeableCluster(t *testing.T) {
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{{Cluster: "uk-app-1", AllocatablePct: 0.30}})

	decision, err := placer.PlaceWith(ukClaim(), "", []string{"uk-app-dark"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster == "uk-app-dark" {
		t.Error("placed into a cluster the platform cannot read — a record saying deployed and a " +
			"reality nobody has checked")
	}
}

// When no preferred cluster can take the work, placement still succeeds — and SAYS it fell back.
//
// Silence here would be the bad outcome: a platform team believes an app is on its named cluster,
// and it is not. The decision is what reaches the chain, so the reason must carry it.
func TestFallingBackPastEveryPreferenceIsReported(t *testing.T) {
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.01},
		{Cluster: "uk-app-2", AllocatablePct: 0.01},
		{Cluster: "uk-app-3", AllocatablePct: 0.80},
	})

	decision, err := placer.PlaceWith(ukClaim(), "", []string{"uk-app-1", "uk-app-2"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster != "uk-app-3" {
		t.Fatalf("expected the fallback cluster; got %q", decision.Cluster)
	}
	if decision.Reason == "" {
		t.Fatal("a fallback with no reason: nobody can tell the app missed its preferred cluster")
	}
	if !strings.Contains(strings.ToLower(decision.Reason), "prefer") {
		t.Errorf("the reason must say a preference was not honoured, or the fallback is silent; "+
			"got %q", decision.Reason)
	}
}

// A running app is NOT migrated because somebody edited a preference.
//
// Stickiness stays ahead of preference for the reason Place already gives: "a placer that
// re-optimised every reconcile pass would migrate live apps whenever capacity shifted — silently,
// unapproved, and catastrophically for anything holding state."
func TestAPreferenceDoesNotMoveARunningApp(t *testing.T) {
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.90},
		{Cluster: "uk-app-2", AllocatablePct: 0.50},
	})

	// It runs on uk-app-2 today. The floor now prefers uk-app-1.
	decision, err := placer.PlaceWith(ukClaim(), "uk-app-2", []string{"uk-app-1"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster != "uk-app-2" {
		t.Errorf("a live app was migrated by a document edit: moved from uk-app-2 to %q. Moving a "+
			"placed app is a new governed decision, never a reconcile outcome", decision.Cluster)
	}
}

// No preference behaves exactly as Place always did.
func TestNoPreferenceIsUnchangedBehaviour(t *testing.T) {
	clusters := clustersForTest()
	capacity := []Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.20},
		{Cluster: "uk-app-2", AllocatablePct: 0.90},
		{Cluster: "uk-app-3", AllocatablePct: 0.50},
	}

	was, err := NewPlacer(clusters).WithCapacity(capacity).Place(ukClaim(), "")
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	now, err := NewPlacer(clusters).WithCapacity(capacity).PlaceWith(ukClaim(), "", nil)
	if err != nil {
		t.Fatalf("placeWith: %v", err)
	}
	if was.Cluster != now.Cluster || was.Reason != now.Reason {
		t.Errorf("PlaceWith with no preference diverged from Place: %+v vs %+v", was, now)
	}
}

// The floor's preference must reach a RESOLVED placement.
//
// Same failure as the gate rule: a floor field nothing reads is worse than an absent one. An
// operator lists clusters, the document loads, and every app lands wherever capacity says.
func TestAnAppClusterPreferenceReachesTheResolvedPlacement(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"prod",
	    "apps":[{"name":"settlement-engine","runtime":"enterprise","image":"h/p/se",
	             "placement":{"env":"prod","purpose":"app","residency":"uk"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.20},
		{Cluster: "uk-app-2", AllocatablePct: 0.90}, // capacity alone picks this
		{Cluster: "uk-app-3", AllocatablePct: 0.50},
	})

	plain, err := ResolveWith(manifest, DefaultFloor(), placer, nil)
	if err != nil {
		t.Fatalf(litResolveFailed, err)
	}
	if got := appClusterIn(t, plain); got != "uk-app-2" {
		t.Fatalf("without a preference, capacity should choose uk-app-2; got %q", got)
	}

	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{
		"payments/settlement-engine": {Clusters: []string{"uk-app-1"}},
	}
	preferred, err := ResolveWith(manifest, floor, placer, nil)
	if err != nil {
		t.Fatalf(litResolveFailed, err)
	}
	if got := appClusterIn(t, preferred); got != "uk-app-1" {
		t.Errorf("the floor's preference did not reach placement: app landed on %q, want uk-app-1",
			got)
	}
}

// A preference for another team's app of the same name must not move this one.
func TestAClusterPreferenceIsTeamQualifiedToo(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{"team":"treasury","owns":"treasury","tier":"prod",
	    "apps":[{"name":"settlement-engine","runtime":"enterprise","image":"h/t/se",
	             "placement":{"env":"prod","purpose":"app","residency":"uk"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	placer := NewPlacer(clustersForTest()).WithCapacity([]Capacity{
		{Cluster: "uk-app-1", AllocatablePct: 0.20},
		{Cluster: "uk-app-2", AllocatablePct: 0.90},
	})
	floor := DefaultFloor()
	floor.Apps = map[string]AppRule{
		"payments/settlement-engine": {Clusters: []string{"uk-app-1"}},
	}

	desired, err := ResolveWith(manifest, floor, placer, nil)
	if err != nil {
		t.Fatalf(litResolveFailed, err)
	}
	if got := appClusterIn(t, desired); got != "uk-app-2" {
		t.Errorf("payments' rule moved treasury's app of the same name to %q", got)
	}
}

// appClusterIn returns the cluster the one app change was placed on.
func appClusterIn(t *testing.T, desired Desired) string {
	t.Helper()
	for _, change := range desired.Changes {
		if change.Asset == "app" {
			return change.Cluster
		}
	}
	t.Fatal("no app change was resolved, so its placement could not be checked")
	return ""
}
