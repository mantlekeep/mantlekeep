package estate

import (
	"context"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// watchPort reports a cluster that already has something running in it.
type watchPort struct{ items []ObservedItem }

func (watchPort) Asset() string   { return "app" }
func (watchPort) Kinds() []string { return []string{"deployment"} }

func (p watchPort) Observe(context.Context, string) (Observed, error) {
	return Observed{Items: p.items}, nil
}

func (watchPort) ApplyApproved(context.Context, mantlekeep.ExecutionToken, DesiredItem) error {
	return nil
}

func existingEstate() Port {
	return Guarded(watchPort{items: []ObservedItem{
		{Asset: "app", Kind: "deployment", Name: "rogue-batch",
			Slot: Slot{Cluster: "c1", Namespace: "payments", Name: "rogue-batch"}},
	}})
}

// THE case this fixes. The first day of any real deployment is an estate that already
// exists and a manifest that does not, and "what is out there that nobody approved?" is the
// first question worth asking. Refusing the read until something is declared made the tool
// useless exactly when it was most needed.
func TestAnUndeclaredTeamStillSeesItsEstate(t *testing.T) {
	service := NewService(DefaultFloor(), NewMemoryManifests(), existingEstate())

	footprint, err := service.Footprint(context.Background(), "payments")
	if err != nil {
		t.Fatalf("an undeclared team was refused: %v", err)
	}
	if footprint.Declared {
		t.Error("Declared must be false — nothing was ever approved for this team")
	}
	if len(footprint.Observed.Items) != 1 {
		t.Fatalf("observed %d items, want 1", len(footprint.Observed.Items))
	}
	if len(footprint.Drifts) != 1 || footprint.Drifts[0].Kind != DriftUnexpected {
		t.Errorf("an undeclared estate must be entirely unexpected, got %+v", footprint.Drifts)
	}
}

// The distinction the old error preserved must survive. "Nothing approved" and "an empty
// estate approved" are different statements, and conflating them is how a typo in a URL
// reads as a clean bill of health.
func TestDeclaringAnEmptyEstateIsNotTheSameAsDeclaringNothing(t *testing.T) {
	store := NewMemoryManifests()
	if err := store.Remember(context.Background(),
		Manifest{Team: "payments", Owns: "payments", Tier: TierDev}); err != nil {
		t.Fatal(err)
	}
	service := NewService(DefaultFloor(), store, existingEstate())

	footprint, err := service.Footprint(context.Background(), "payments")
	if err != nil {
		t.Fatalf("Footprint: %v", err)
	}
	if !footprint.Declared {
		t.Error("a team that declared an empty estate must report Declared true")
	}
}

// Manifest() keeps returning the error: a caller asking specifically for the declaration
// must still be able to tell that there is none.
func TestManifestStillReportsAnUndeclaredTeam(t *testing.T) {
	service := NewService(DefaultFloor(), NewMemoryManifests(), existingEstate())
	if _, err := service.Manifest(context.Background(), "payments"); err == nil {
		t.Fatal("Manifest must still distinguish a team that declared nothing")
	}
}
