package estate

import (
	"strings"
	"testing"
)

const litNamespace = "namespace: %v"

// resolvedSlot resolves one payments app onto a single sit cluster with the given pattern.
func resolvedSlot(t *testing.T, pattern NamespacePattern) (DesiredItem, error) {
	t.Helper()
	manifest, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"api","runtime":"enterprise","image":"h/p/api",
	             "placement":{"env":"sit","purpose":"app","residency":"uk"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	placer := NewPlacer([]Cluster{{Name: "alpha-sit", Env: "sit", Purpose: "app",
		Residency: "uk", Reachable: true, Namespace: pattern}})
	desired, err := ResolveWith(manifest, DefaultFloor(), placer, nil)
	if err != nil {
		return DesiredItem{}, err
	}
	for _, item := range desired.Changes {
		if item.Asset == "app" {
			return item, nil
		}
	}
	t.Fatal("no app was resolved")
	return DesiredItem{}, nil
}

// No pattern keeps the namespace the manifest's owns, as before.
func TestNoPatternKeepsTheOwnsNamespace(t *testing.T) {
	item, err := resolvedSlot(t, "")
	if err != nil {
		t.Fatalf(litResolveFailed, err)
	}
	if item.Slot.Namespace != "payments" {
		t.Errorf("namespace %q, want payments", item.Slot.Namespace)
	}
}

// A pattern places the app in the rendered namespace; the deployment name and team are unchanged.
func TestAPatternPlacesTheAppInItsNamespace(t *testing.T) {
	item, err := resolvedSlot(t, "{env}-{owns}")
	if err != nil {
		t.Fatalf(litResolveFailed, err)
	}
	if item.Slot.Namespace != "sit-payments" {
		t.Errorf("namespace %q, want sit-payments", item.Slot.Namespace)
	}
	if item.Name != "payments-api" || item.Team != "payments" {
		t.Errorf("name %q team %q, want payments-api and payments", item.Name, item.Team)
	}
}

// A literal pattern puts every team in one shared namespace.
func TestALiteralPatternIsOneSharedNamespace(t *testing.T) {
	item, err := resolvedSlot(t, "sit-workloads")
	if err != nil {
		t.Fatalf(litResolveFailed, err)
	}
	if item.Slot.Namespace != "sit-workloads" {
		t.Errorf("namespace %q, want sit-workloads", item.Slot.Namespace)
	}
}

// A pattern rendering an invalid namespace refuses the app rather than truncating it.
func TestAnInvalidRenderedNamespaceIsRefused(t *testing.T) {
	for _, pattern := range []NamespacePattern{"{owns}-" + NamespacePattern(strings.Repeat("x", 60)), "Sit-{owns}", "{team}"} {
		if _, err := resolvedSlot(t, pattern); err == nil {
			t.Errorf("pattern %q resolved; want a refusal", pattern)
		}
	}
}

// Validate knows {owns} and {env} only.
func TestValidateRefusesAnUnknownPlaceholder(t *testing.T) {
	for pattern, valid := range map[NamespacePattern]bool{
		"": true, "{owns}": true, "{env}-{owns}": true, "shared": true,
		"{team}": false, "{owns": false, "sit-{cluster}": false,
	} {
		if err := pattern.Validate(); (err == nil) != valid {
			t.Errorf(litNamespace+" for %q, want valid=%v", err, pattern, valid)
		}
	}
}
