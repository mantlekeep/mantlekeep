package estate

import (
	"strings"
	"testing"
)

// A claim may name the labels a cluster must carry instead of an environment.
//
// # Why this exists
//
// The placer used to insist that an estate is organised by environment: a claim with no Env was
// refused outright. A deployment organised by region, tenancy or data classification could not
// place at all. Requires lets the deployment choose its own dimensions; the named fields keep
// working, so nothing that placed before places differently now.
//
// Two rules the generic matcher keeps, each asserted below: EVERY required label must match, and
// an absent label is not a wildcard. Residency stays its own field and cannot be reached through
// a label.

// labelledFleet is organised by tenancy and data classification, with no environment at all.
func labelledFleet() []Cluster {
	return []Cluster{
		{Name: "eu-payments-restricted", Residency: "eu", Reachable: true, Labels: map[string]string{
			"region": "eu", "tenancy": "payments", "classification": "restricted"}},
		{Name: "eu-payments-internal", Residency: "eu", Reachable: true, Labels: map[string]string{
			"region": "eu", "tenancy": "payments", "classification": "internal"}},
		{Name: "eu-ledger-restricted", Residency: "eu", Reachable: true, Labels: map[string]string{
			"region": "eu", "tenancy": "ledger", "classification": "restricted"}},
	}
}

func restrictedPaymentsClaim() Placement {
	return Placement{
		Residency: "eu",
		Requires:  map[string]string{"tenancy": "payments", "classification": "restricted"},
	}
}

// An estate organised by something OTHER than environment places, on the one cluster that
// carries every required label.
func TestAnEstateOrganisedByAnythingAtAll(t *testing.T) {
	chosen, err := NewPlacer(labelledFleet()).Place(restrictedPaymentsClaim(), "")
	if err != nil {
		t.Fatalf("a claim with no environment at all was refused: %v", err)
	}
	if chosen.Cluster != "eu-payments-restricted" {
		t.Errorf("placed on %q — the labels name exactly one cluster", chosen.Cluster)
	}
	if len(chosen.Considered) != 1 {
		t.Errorf("considered %v, want only the cluster matching every label", chosen.Considered)
	}
}

// EVERY required label must match. A cluster matching one of two is not a match.
func TestEveryRequiredLabelMustMatch(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "partial", Residency: "eu", Reachable: true,
			Labels: map[string]string{"tenancy": "payments", "classification": "internal"}},
	})
	if _, err := placer.Place(restrictedPaymentsClaim(), ""); err == nil {
		t.Fatal("a cluster matching one label of two was placed on")
	}
}

// A cluster that does not carry the key AT ALL is not a match. A claim asking for
// tenancy=payments must not land on a cluster that never said which tenancy it serves.
func TestAnAbsentLabelIsNotAWildcard(t *testing.T) {
	placer := NewPlacer([]Cluster{{Name: "unlabelled", Residency: "eu", Reachable: true}})
	if _, err := placer.Place(Placement{
		Residency: "eu", Requires: map[string]string{"tenancy": "payments"},
	}, ""); err == nil {
		t.Fatal("a cluster with no tenancy label satisfied a claim requiring one")
	}
}

// An EMPTY required value is not a wildcard either. Without this, requires:{"region":""} would
// pass the "say where you belong" check and then match every cluster that never named a region.
func TestAnEmptyRequiredValueMatchesNothing(t *testing.T) {
	placer := NewPlacer([]Cluster{{Name: "unlabelled", Residency: "eu", Reachable: true}})
	if _, err := placer.Place(Placement{
		Residency: "eu", Requires: map[string]string{"tenancy": ""},
	}, ""); err == nil {
		t.Fatal("an empty required label matched a cluster that does not carry it")
	}
}

// A cluster's NAME is not a label. Requires comes from the team's manifest, and a team that could
// require a cluster by name would be choosing its own placement.
func TestAClaimCannotNameItsClusterThroughLabels(t *testing.T) {
	placer := NewPlacer([]Cluster{{Name: "uk-app-1", Residency: "uk", Reachable: true}})
	for _, key := range []string{"name", "cluster"} {
		if _, err := placer.Place(Placement{
			Residency: "uk", Requires: map[string]string{key: "uk-app-1"},
		}, ""); err == nil {
			t.Errorf("requires:{%q:\"uk-app-1\"} chose a cluster by name", key)
		}
	}
}

// RESIDENCY IS NOT A LABEL. Requiring it through Requires must not be a way to place data
// outside the jurisdiction the claim's own Residency field confines it to.
func TestResidencyCannotBeBypassedThroughLabels(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "eu-only", Residency: "eu", Reachable: true,
			Labels: map[string]string{"tenancy": "payments"}},
	})
	// EU through the label map, region-a through the real field. The real field must win,
	// because it is the one carrying the guarantee.
	if _, err := placer.Place(Placement{
		Residency: "region-a",
		Requires:  map[string]string{"tenancy": "payments", "residency": "eu"},
	}, ""); err == nil {
		t.Fatal("a label claiming residency=eu placed region-a data on an EU cluster — " +
			"residency was bypassed through the generic matcher")
	}
}

// A claim that names neither an environment nor any label is refused. It would match the whole
// fleet, and "anywhere" is not a placement anybody ruled on.
func TestAClaimThatSaysNothingIsRefused(t *testing.T) {
	placer := NewPlacer([]Cluster{{Name: "any", Residency: "eu", Reachable: true}})
	_, err := placer.Place(Placement{Residency: "eu"}, "")
	if err == nil {
		t.Fatal("a claim with no environment and no labels was placed")
	}
	// The refusal must offer BOTH forms, or somebody with an existing document is told they did
	// something wrong when they did not.
	if !strings.Contains(err.Error(), "environment") || !strings.Contains(err.Error(), "requires") {
		t.Errorf("the refusal does not offer both forms: %v", err)
	}
}

// The named fields keep working, unchanged: every existing fleet document and manifest behaves
// exactly as before.
func TestTheNamedFormStillWorksUnchanged(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "sit-a", Env: "sit", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "uat-a", Env: "uat", Purpose: "app", Residency: "region-a", Reachable: true},
	})
	chosen, err := placer.Place(Placement{Env: "sit", Purpose: "app", Residency: "region-a"}, "")
	if err != nil {
		t.Fatalf("an existing claim stopped working: %v", err)
	}
	if chosen.Cluster != "sit-a" {
		t.Errorf("placed on %q", chosen.Cluster)
	}
}

// The two forms are interchangeable: requires:{"env":"sit"} finds a cluster whose Env field says
// sit, with no labels set. A deployment can migrate one document at a time.
func TestTheGenericFormReadsTheNamedFields(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "sit-a", Env: "sit", Purpose: "app", Residency: "region-a", Reachable: true},
		{Name: "uat-a", Env: "uat", Purpose: "app", Residency: "region-a", Reachable: true},
	})
	chosen, err := placer.Place(Placement{
		Residency: "region-a",
		Requires:  map[string]string{"env": "sit", "purpose": "app"},
	}, "")
	if err != nil {
		t.Fatalf("a generic claim could not read the named fields: %v", err)
	}
	if chosen.Cluster != "sit-a" {
		t.Errorf("placed on %q", chosen.Cluster)
	}
}

// An unreachable cluster stays unreachable however it is selected. Placing blind produces a
// record that says deployed and a reality nobody checked.
func TestLabelsDoNotMakeAnUnreachableClusterLegal(t *testing.T) {
	placer := NewPlacer([]Cluster{
		{Name: "dark", Residency: "eu", Reachable: false,
			Labels: map[string]string{"tenancy": "payments"}},
	})
	if _, err := placer.Place(Placement{
		Residency: "eu", Requires: map[string]string{"tenancy": "payments"},
	}, ""); err == nil {
		t.Fatal("a cluster nobody could read was placed into")
	}
}

// Labels and the platform's preference compose: labels decide what is LEGAL, the preference
// chooses among what is legal. A preferred cluster that fails a required label is skipped
// exactly as an out-of-jurisdiction one is, and plan B is taken.
func TestAPreferenceIsAppliedOnlyAmongLabelLegalClusters(t *testing.T) {
	fleet := append(labelledFleet(), Cluster{Name: "eu-payments-restricted-2", Residency: "eu",
		Reachable: true, Labels: map[string]string{
			"region": "eu", "tenancy": "payments", "classification": "restricted"}})
	placer := NewPlacer(fleet)

	decision, err := placer.PlaceWith(restrictedPaymentsClaim(), "",
		[]string{"eu-ledger-restricted", "eu-payments-restricted-2"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster != "eu-payments-restricted-2" {
		t.Errorf("placed on %q, want the first preferred cluster that carries every label",
			decision.Cluster)
	}
	if !strings.Contains(decision.Reason, "preferred") {
		t.Errorf("reason %q does not say the preference was honoured", decision.Reason)
	}
}

// When every preferred cluster fails a required label, placement falls through to capacity and
// says so — a preference can never widen what the labels allow.
func TestAPreferenceCannotReachPastTheLabels(t *testing.T) {
	placer := NewPlacer(labelledFleet())

	decision, err := placer.PlaceWith(restrictedPaymentsClaim(), "",
		[]string{"eu-ledger-restricted", "eu-payments-internal"})
	if err != nil {
		t.Fatalf(litPlaceFailed, err)
	}
	if decision.Cluster != "eu-payments-restricted" {
		t.Errorf("placed on %q — a preference reached a cluster the labels refused", decision.Cluster)
	}
	if !strings.HasPrefix(decision.Reason, "no preferred cluster") {
		t.Errorf("reason %q hides that the preference was not honoured", decision.Reason)
	}
}
