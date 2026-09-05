package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// withGates splices a gates block into the known-good document, so these tests differ from the
// passing one by exactly the thing under test.
func withGates(t *testing.T, gates string) string {
	t.Helper()
	const anchor = `    "fleet": {`
	if !strings.Contains(validDocument, anchor) {
		t.Fatalf("fixture changed shape — the gates block has nowhere to go")
	}
	return strings.Replace(validDocument, anchor, `    "gates": `+gates+`,
`+anchor, 1)
}

func TestADeploymentMayRaiseAGateOnATierTheDefaultTreatsAsHarmless(t *testing.T) {
	// The case this exists for: a shared DEV cluster that ten teams depend on is not the
	// playground the built-in ladder assumes, and saying so must not cost a release.
	config, err := Parse([]byte(withGates(t, `{"dev": "owning-team"}`)))
	if err != nil {
		t.Fatalf("raising dev to owning-team was refused: %v", err)
	}
	if got := config.Floor.GateFor(estate.TierDev); got != estate.GateOwningTeam {
		t.Errorf("dev gate = %q, want owning-team", got)
	}
	// The tiers the document did not mention keep their gates rather than being dropped.
	if got := config.Floor.GateFor(estate.TierProd); got != estate.GatePlatform {
		t.Errorf("prod gate = %q, want platform — naming one tier must not un-gate the rest", got)
	}
}

func TestAConfigCannotLowerAGateBelowTheBuiltInLadder(t *testing.T) {
	// The floor. Every limit in this document is sound; the gate alone is the breach, and it
	// must be enough to refuse the whole file.
	_, err := Parse([]byte(withGates(t, `{"prod": "none"}`)))
	if err == nil {
		t.Fatal("a config un-gated production and was accepted — config reached the guarantee")
	}
	if !strings.Contains(err.Error(), "cannot lower the floor") {
		t.Errorf("error does not say why: %v", err)
	}
}

func TestAnUnrecognisedGateIsRefusedRatherThanAssumedPermissive(t *testing.T) {
	_, err := Parse([]byte(withGates(t, `{"prod": "probably-fine"}`)))
	if err == nil {
		t.Fatal("an unknown gate was accepted — an unrecognised gate must never resolve")
	}
}

func TestTheFloorRevisionIsDerivedFromContentAndCannotBeDeclared(t *testing.T) {
	first, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	again, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if first.Floor.Revision == "" {
		t.Fatal("no revision — a decision made under this floor could not name which floor")
	}
	if first.Floor.Revision != again.Floor.Revision {
		t.Error("the same document produced two revisions — two servers on one file would " +
			"disagree about which rules they are running")
	}

	edited, err := Parse([]byte(withGates(t, `{"dev": "owning-team"}`)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if edited.Floor.Revision == first.Floor.Revision {
		t.Error("an edited floor kept the old revision — an operator's change would be " +
			"invisible in every record it affected")
	}

	// Declared, it could be forgotten on an edit. The document has no such field, so the same
	// unknown-field refusal that guards every other typo guards this too.
	if _, err := Parse([]byte(strings.Replace(validDocument, `"ownership": {}`,
		`"ownership": {}, "revision": "hand-written"`, 1))); err == nil {
		t.Error("a document declared its own revision and was accepted")
	}
}

func TestABadFileOnReloadLeavesThePreviousFloorServing(t *testing.T) {
	// A typo must not be able to un-govern a running footprint, and refusing to serve at all
	// would turn an operator's slip into an outage.
	path := filepath.Join(t.TempDir(), "floor.json")
	if err := os.WriteFile(path, []byte(validDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	live, err := OpenLive(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	good := live.Floor().Revision

	if err := os.WriteFile(path, []byte(`{"floor": {`), 0o600); err != nil {
		t.Fatal(err)
	}
	inForce, err := live.Reload()
	if err == nil {
		t.Fatal("a truncated document reloaded cleanly")
	}
	if inForce.Floor.Revision != good {
		t.Errorf("reload reported revision %q, want the previous %q — an operator must be told "+
			"what is actually governing", inForce.Floor.Revision, good)
	}
	if live.Floor().Revision != good {
		t.Error("the live floor changed on a FAILED reload — half a floor is in force")
	}
	if live.Floor().Kafka[estate.TierDev].Retention == 0 {
		t.Error("the previous limits were lost, so the estate is now governed by nothing")
	}
}

func TestAGoodReloadReplacesTheFloorWithoutARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "floor.json")
	if err := os.WriteFile(path, []byte(validDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	live, err := OpenLive(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := live.Floor().GateFor(estate.TierDev); got != estate.GateNone {
		t.Fatalf("dev gate = %q, want none before the edit", got)
	}

	if err := os.WriteFile(path, []byte(withGates(t, `{"dev": "owning-team"}`)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := live.Floor().GateFor(estate.TierDev); got != estate.GateOwningTeam {
		t.Errorf("dev gate = %q after reload, want owning-team — the reload reported success "+
			"and changed nothing", got)
	}
}
