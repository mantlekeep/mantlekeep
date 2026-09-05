package estate

import (
	"strings"
	"testing"
)

func fleet() []Cluster {
	return []Cluster{
		{Name: "dev-app-region-a-1", Provider: "gke", Region: "region-a", Env: "dev", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "dev-app-uk-2", Provider: "gke", Region: "region-a", Env: "dev", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "dev-core-region-a-1", Provider: "internal", Region: "region-a", Env: "dev", Purpose: "core", Residency: "region-a", Reachable: true},
		{Name: "dev-app-region-b-1", Provider: "provider-c", Region: "region-b", Env: "dev", Purpose: "app", Residency: "region-b", Reachable: true},
		{Name: "sit-app-region-a-1", Provider: "eks", Region: "region-a", Env: "sit", Purpose: "app", Residency: "region-a", Reachable: true},
	}
}

// THE rule that cannot be undone. A quota breach is noisy and a wrong deployment is
// redeployable; personal data landing in an unapproved jurisdiction is a reportable
// incident nothing reverses.
func TestUKDataIsNeverPlacedInHongKong(t *testing.T) {
	placer := NewPlacer(fleet())

	decision, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "")
	if err != nil {
		t.Fatalf("a legal claim must place: %v", err)
	}
	for _, considered := range decision.Considered {
		if strings.Contains(considered, "region-b") {
			t.Fatalf("an HK cluster was considered for UK data: %v", decision.Considered)
		}
	}
	if strings.Contains(decision.Cluster, "region-b") {
		t.Fatalf("UK data was placed in HK: %s", decision.Cluster)
	}
}

// Capacity pressure must never be able to push data across a jurisdiction. This is the case
// that matters: the only legal cluster is nearly full, and the illegal one is empty.
func TestCapacityPressureCannotBreachResidency(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "dev-app-region-b-1", Env: "dev", Purpose: "app", Residency: "region-b", Reachable: true},
	}).WithCapacity([]Capacity{
		{Cluster: "dev-app-region-a-1", AllocatablePct: 0.02}, // nearly full
		{Cluster: "dev-app-region-b-1", AllocatablePct: 0.95}, // wide open, and illegal
	})

	_, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "")
	if err == nil {
		t.Fatal("placement succeeded — it must REFUSE rather than reach for the empty HK cluster")
	}
	if strings.Contains(err.Error(), "region-b") {
		t.Fatalf("the HK cluster was even a candidate: %v", err)
	}
	if !strings.Contains(err.Error(), "never schedule") {
		t.Fatalf("the refusal must say why; got %v", err)
	}
}

// An unstated residency is a claim nobody has ruled on. Defaulting it to "anywhere" is how
// personal data ends up in the wrong jurisdiction by omission rather than by decision.
func TestAClaimWithNoResidencyIsRefused(t *testing.T) {
	_, err := NewPlacer(fleet()).Place(Placement{Env: "dev", Purpose: "app"}, "")
	if err == nil {
		t.Fatal("a claim with no residency was placed — silence became permission")
	}
}

// STICKINESS. A placer that re-optimises every reconcile pass migrates live apps whenever
// capacity shifts: silently, unapproved, and catastrophically for anything holding state.
func TestAPlacedAppStaysPutEvenWhenAnEmptierClusterExists(t *testing.T) {
	placer := NewPlacer(fleet()).WithCapacity([]Capacity{
		{Cluster: "dev-app-region-a-1", AllocatablePct: 0.20}, // where it already runs
		{Cluster: "dev-app-uk-2", AllocatablePct: 0.90},       // emptier, and irrelevant
	})

	decision, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "dev-app-region-a-1")
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if decision.Cluster != "dev-app-region-a-1" {
		t.Fatalf("a running app was migrated to %q by a reconcile pass — moving a placed app "+
			"is a NEW governed decision, never an optimisation", decision.Cluster)
	}
	if !decision.Sticky {
		t.Fatal("the decision must record that it kept an existing placement")
	}
}

// Stickiness ends where legality does: if the current cluster stops being permitted, it moves.
func TestAnAppMovesWhenItsClusterBecomesIllegal(t *testing.T) {
	placer := NewPlacer(fleet())

	decision, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"},
		"dev-app-region-b-1") // was in HK; no longer permitted for UK data
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if strings.Contains(decision.Cluster, "region-b") {
		t.Fatal("it stayed on an illegal cluster — stickiness must not outrank residency")
	}
	if !strings.Contains(decision.Reason, "moved") {
		t.Fatalf("a move must say it moved; got %q", decision.Reason)
	}
}

// The overflow case: two clusters of the same type and region, one full.
func TestOverflowPicksTheEmptierOfTwoIdenticalClusters(t *testing.T) {
	placer := NewPlacer(fleet()).WithCapacity([]Capacity{
		{Cluster: "dev-app-region-a-1", AllocatablePct: 0.05},
		{Cluster: "dev-app-uk-2", AllocatablePct: 0.60},
	})

	decision, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "")
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if decision.Cluster != "dev-app-uk-2" {
		t.Fatalf("the full cluster was chosen; got %q", decision.Cluster)
	}
}

// A Deployment that exists but whose pods can never schedule is the worst failure shape: it
// looks deployed.
func TestAClusterAtItsLimitIsNotACandidate(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app", Residency: "region-a", Reachable: true},
	}).WithCapacity([]Capacity{{Cluster: "dev-app-region-a-1", AllocatablePct: 0.01}})

	if _, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, ""); err == nil {
		t.Fatal("placed into a cluster with no room — the pods would never schedule")
	}
}

// Placing into a cluster we cannot read is placing blind: the record says deployed and nobody
// has checked.
func TestAnUnreachableClusterIsNeverPlacedInto(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app", Residency: "region-a", Reachable: false},
	})

	if _, err := placer.Place(Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, ""); err == nil {
		t.Fatal("placed into an unreachable cluster")
	}
}

// Purpose separates app from core, so a core cluster is not an overflow target for an app.
func TestPurposeSeparatesAppFromCore(t *testing.T) {
	decision, err := NewPlacer(fleet()).Place(
		Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "")
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	for _, considered := range decision.Considered {
		if strings.Contains(considered, "core") {
			t.Fatalf("a core cluster was considered for an app: %v", decision.Considered)
		}
	}
}

// The decision must be reviewable: which alternatives existed, and why this one.
func TestTheDecisionRecordsWhatItConsidered(t *testing.T) {
	decision, err := NewPlacer(fleet()).Place(
		Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "")
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if len(decision.Considered) != 2 {
		t.Fatalf("both permitted UK app clusters must be recorded; got %v", decision.Considered)
	}
	if decision.Reason == "" {
		t.Fatal("a placement with no reason cannot be reviewed — nobody can answer why here")
	}
}

// The manifest path end to end: a claim in, a recorded platform decision out. Testing the
// placer alone would not have caught Resolve ignoring it.
func TestAManifestClaimBecomesARecordedPlacement(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"api","runtime":"enterprise","image":"h/p/api",
	             "placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	desired, err := ResolveWith(m, DefaultFloor(), NewPlacer(fleet()), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	for _, change := range desired.Changes {
		if change.Asset != "app" {
			continue
		}
		if change.Placement == nil {
			t.Fatal("the placement decision did not survive resolve — where an app runs is a " +
				"platform choice, so it must reach the chain")
		}
		if change.Slot.Cluster != change.Placement.Cluster {
			t.Fatalf("the slot and the decision disagree: %q vs %q",
				change.Slot.Cluster, change.Placement.Cluster)
		}
		if len(change.Placement.Considered) == 0 {
			t.Fatal("the alternatives must be recorded so the choice can be reviewed")
		}
		return
	}
	t.Fatal("no app change resolved")
}

// Stickiness has to survive the manifest path too: the reconciler passes where things already
// run, and a re-resolve must not move them.
func TestResolveKeepsAnAppWhereItAlreadyRuns(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"api","runtime":"enterprise","image":"h/p/api",
	             "placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	placer := NewPlacer(fleet()).WithCapacity([]Capacity{
		{Cluster: "dev-app-region-a-1", AllocatablePct: 0.15},
		{Cluster: "dev-app-uk-2", AllocatablePct: 0.95}, // emptier, and irrelevant
	})

	desired, err := ResolveWith(m, DefaultFloor(), placer,
		map[string]string{"api": "dev-app-region-a-1"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	for _, change := range desired.Changes {
		if change.Asset != "app" {
			continue
		}
		if change.Slot.Cluster != "dev-app-region-a-1" {
			t.Fatalf("a re-resolve migrated a running app to %q — reconciling must never move "+
				"a placed app", change.Slot.Cluster)
		}
		if !change.Placement.Sticky {
			t.Fatal("the decision must record that it kept an existing placement")
		}
		return
	}
	t.Fatal("no app change resolved")
}

// A manifest that names no jurisdiction is refused at PARSE, before any cluster is considered.
func TestAManifestWithoutResidencyNeverReachesThePlacer(t *testing.T) {
	_, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"api","runtime":"enterprise","image":"h/p/api",
	             "placement":{"env":"dev","purpose":"app"}}]}`))
	if err == nil {
		t.Fatal("an app with no residency parsed — silence became permission")
	}
	if !strings.Contains(err.Error(), "by omission") {
		t.Fatalf("the refusal must say why; got %v", err)
	}
}
