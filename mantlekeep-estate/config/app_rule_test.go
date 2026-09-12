package config

import (
	"strings"
	"testing"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// An app rule is loaded, and a rule that would weaken a gate is REFUSED at load.
//
// Resolution ignores a weaker rule anyway, so this is not what makes the guarantee. It is what
// makes the mistake visible: a silently-ignored rule is worse than a refused one, because an
// operator writes "gate": "none" against an app, the file loads, and they believe they have
// exempted something. They find out from an approval queue instead of from an error.

// withApps splices an apps block into the known-good document, so each case differs from a
// PASSING document by exactly the thing under test — not by an incomplete floor that would fail
// earlier, for a reason that has nothing to do with app rules.
func withApps(t *testing.T, apps string) string {
	t.Helper()
	const anchor = `    "fleet": {`
	if !strings.Contains(validDocument, anchor) {
		t.Fatalf("fixture changed shape — the apps block has nowhere to go")
	}
	return strings.Replace(validDocument, anchor, `    "apps": `+apps+`,
`+anchor, 1)
}

func TestAnAppRuleIsLoaded(t *testing.T) {
	config, err := Parse([]byte(withApps(t, `{ "payments/settlement-engine": { "gate": "platform" } }`)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rule, ok := config.Floor.Apps["payments/settlement-engine"]
	if !ok {
		t.Fatal("the apps table did not survive loading: a rule an operator wrote reached nothing")
	}
	if rule.Gate != estate.GatePlatform {
		t.Errorf("gate = %q, want %q", rule.Gate, estate.GatePlatform)
	}
}

func TestAWeakerAppRuleIsRefusedAtLoad(t *testing.T) {
	_, err := Parse([]byte(withApps(t, `{ "payments/checkout": { "gate": "none" } }`)))
	if err == nil {
		t.Fatal("a rule weakening a gate was accepted — naming an app would be the way out of " +
			"approval, and the operator would be told nothing")
	}
	if !strings.Contains(err.Error(), "can never lower") {
		t.Errorf("the error must say which direction is refused; got: %v", err)
	}
}

func TestAnUnqualifiedAppKeyIsRefused(t *testing.T) {
	_, err := Parse([]byte(withApps(t, `{ "checkout": { "gate": "platform" } }`)))
	if err == nil {
		t.Fatal("a bare app name was accepted: the rule would reach every team's app of that name")
	}
	if !strings.Contains(err.Error(), "team-qualified") {
		t.Errorf("the error must name the fix; got: %v", err)
	}
}

func TestAnUnknownGateInAnAppRuleIsRefusedAtLoad(t *testing.T) {
	_, err := Parse([]byte(withApps(t, `{ "payments/checkout": { "gate": "rubber-stamp" } }`)))
	if err == nil {
		t.Fatal("an unrecognised gate was accepted — it would be ranked zero and quietly ignored")
	}
}

// A document with no apps table is complete, not incomplete.
func TestAFloorWithNoAppsTableLoads(t *testing.T) {
	config, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(config.Floor.Apps) != 0 {
		t.Errorf("an absent apps table must produce no rules; got %d", len(config.Floor.Apps))
	}
}

// A rule that names no gate is legitimate and changes nothing.
//
// The shape exists so a later field — a cluster preference, an extra approver — is an added field
// rather than a changed document for every deployment already running one.
func TestAnAppRuleWithNoGateLoads(t *testing.T) {
	config, err := Parse([]byte(withApps(t, `{ "payments/checkout": {} }`)))
	if err != nil {
		t.Fatalf("a rule naming no gate must load: %v", err)
	}
	if got := config.Floor.GateForApp(estate.TierProd, "payments/checkout"); got != estate.GatePlatform {
		t.Errorf("it must leave the tier's gate alone; got %q", got)
	}
}
