package estate

import (
	"strings"
	"testing"
)

// fleetFor builds a placer over one cluster per environment, so a manifest can target any of
// them and the only variable under test is the tier it gets governed at.
func fleetFor(t *testing.T) *Placer {
	t.Helper()
	clusters := []Cluster{
		{Name: "dev-1", Env: "dev", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "sit-1", Env: "sit", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "prod-1", Env: "prod", Purpose: "app", Residency: "region-a", Reachable: true},
	}
	return NewPlacer(clusters)
}

func appManifest(t *testing.T, tier, env string) Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"` + tier + `",
	  "apps":[{"name":"api","runtime":"enterprise","image":"harbor/payments/api",
	           "placement":{"env":"` + env + `","purpose":"app","residency":"region-a"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

func appItem(t *testing.T, desired Desired) DesiredItem {
	t.Helper()
	for _, change := range desired.Changes {
		if change.Asset == "app" {
			return change
		}
	}
	t.Fatal("no app change resolved")
	return DesiredItem{}
}

// THE HOLE THIS CLOSES. Tier is declared by the team and picks BOTH the gate and the limits, so
// a manifest asking for "dev" against a production cluster used to resolve to no gate and
// dev-sized limits inside prod — the request choosing its own guarantee.
func TestAProductionEnvironmentCannotBeGovernedAtDevTier(t *testing.T) {
	floor := DefaultFloor()
	desired, err := ResolveWith(appManifest(t, "dev", "prod"), floor, fleetFor(t), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	item := appItem(t, desired)

	if item.Tier != TierProd {
		t.Errorf("tier = %q for a prod-env app declared dev — the environment must raise it", item.Tier)
	}
	if item.Gate != GatePlatform {
		t.Errorf("gate = %q, want platform — a dev tier in prod bought no approval at all", item.Gate)
	}
	prod := floor.App[RuntimeEnterprise][TierProd]
	if got, ok := item.Limits.(AppLimits); !ok || got != prod {
		t.Errorf("limits = %+v, want the prod floor %+v — dev-sized limits landed in production",
			item.Limits, prod)
	}
}

func TestAnAppMayBeGovernedMoreStrictlyThanItsEnvironmentDemands(t *testing.T) {
	// Raising is the only direction. Asking for prod discipline in dev is a team's own choice
	// and must survive.
	desired, err := ResolveWith(appManifest(t, "prod", "dev"), DefaultFloor(), fleetFor(t), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := appItem(t, desired).Tier; got != TierProd {
		t.Errorf("tier = %q, want prod — the environment lowered a tier the team chose", got)
	}
}

func TestAnEnvironmentTheFloorHasNotRuledOnIsRefusedRatherThanGuessed(t *testing.T) {
	// An env nobody configured is a gap in the floor, not a licence. Guessing would govern it
	// at whatever tier the request asked for, which is the hole by another route.
	_, err := ResolveWith(appManifest(t, "dev", "playground"), DefaultFloor(), fleetFor(t), nil)
	if err == nil {
		t.Fatal("an unruled environment resolved — it would be governed at the requested tier")
	}
	if !strings.Contains(err.Error(), "has not ruled on") {
		t.Errorf("error does not name the cause: %v", err)
	}
}

func TestTheEnvironmentTheTierWasAppliedToIsTheOnePlacedInto(t *testing.T) {
	desired, err := ResolveWith(appManifest(t, "dev", "sit"), DefaultFloor(), fleetFor(t), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	item := appItem(t, desired)
	if item.Placement == nil || item.Placement.Env != "sit" {
		t.Fatalf("placement did not record its environment: %+v", item.Placement)
	}
	if item.Tier != TierShared {
		t.Errorf("tier = %q for env sit, want shared", item.Tier)
	}
}
