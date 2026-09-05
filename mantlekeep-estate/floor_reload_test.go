package estate

import (
	"context"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// A reloaded floor has to change the ANSWER, not just the value in a struct. This drives the
// door with a floor that changes underneath a live manager, which is the only way to tell a
// working reload from one that reports success and governs under the boot config forever.
func TestAReloadedFloorChangesTheGateOnTheNextChange(t *testing.T) {
	floor := DefaultFloor()
	door := &fakeDoor{}
	port := &recordingPort{asset: "kafka"}

	manager := NewManager(door, floor, port).FloorFrom(func() Floor { return floor })
	actor := mantlekeep.Subject{ID: "dev-alice"}
	manifest, err := ParseManifest([]byte(
		`{"team":"payments","owns":"payments","tier":"dev","kafka":{"topics":["orders"]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if _, err := manager.Apply(context.Background(), actor, manifest); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := door.submitted[0].Params["gate"]; got != string(GateNone) {
		t.Fatalf("gate = %v before the reload, want none", got)
	}

	// The operator raises the shared DEV env's gate and reloads. No restart, no redeploy.
	floor.Gates = map[Tier]Gate{TierDev: GateOwningTeam, TierShared: GateOwningTeam,
		TierProd: GatePlatform}

	door.submitted = nil
	if _, err := manager.Apply(context.Background(), actor, manifest); err != nil {
		t.Fatalf("apply after reload: %v", err)
	}
	if got := door.submitted[0].Params["gate"]; got != string(GateOwningTeam) {
		t.Errorf("gate = %v after the reload, want owning-team — the manager is still "+
			"governing under the floor it booted with", got)
	}
	// A gated SUBMISSION still names no requester. This assertion used to demand the opposite,
	// and demanding it was the defect: with subject == requester the door's separation-of-duties
	// rule fired on the request rather than on an approval, so raising the gate here turned a
	// working change into a flat denial nobody could ever clear. The requester belongs on the
	// APPROVAL call — see TestAnApprovalCarriesBothNamesSoTheDoorCanEnforceSoD.
	if got := door.submitted[0].Params["requester"]; got != "" {
		t.Errorf("requester = %q on a submission — a submission that claims to be its own "+
			"approval is refused before anybody can approve it", got)
	}
}

func TestEveryDecisionRecordsWhichFloorMadeIt(t *testing.T) {
	// Without this a grant recorded a year ago cannot be read against the rules that were
	// actually in force, and an approval today's floor would refuse is indistinguishable
	// from an error.
	floor := DefaultFloor()
	floor.Revision = "abc123def456"
	door := &fakeDoor{}
	manager := NewManager(door, floor, &recordingPort{asset: "kafka"})

	manifest, err := ParseManifest([]byte(
		`{"team":"payments","owns":"payments","tier":"dev","kafka":{"topics":["orders"]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := manager.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"},
		manifest); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(door.submitted) == 0 {
		t.Fatal("nothing reached the door")
	}
	for _, intent := range door.submitted {
		if got := intent.Params["floorRevision"]; got != "abc123def456" {
			t.Errorf("floorRevision = %v on intent %s, want abc123def456 — the record cannot "+
				"say which rules decided", got, intent.ID)
		}
	}
}

// A floor read per change rather than per request would let a reload land mid-manifest, making
// one manifest's decisions half one revision and half another — reproducible by nobody.
func TestOneManifestIsGovernedUnderExactlyOneFloorRevision(t *testing.T) {
	revisions := []string{"rev-one", "rev-two", "rev-three"}
	reads := 0
	floorOf := func() Floor {
		f := DefaultFloor()
		f.Revision = revisions[min(reads, len(revisions)-1)]
		reads++
		return f
	}

	door := &fakeDoor{}
	manager := NewManager(door, DefaultFloor(), &recordingPort{asset: "kafka"}).FloorFrom(floorOf)
	manifest, err := ParseManifest([]byte(
		`{"team":"payments","owns":"payments","tier":"dev",
		  "kafka":{"topics":["orders","payments","refunds"]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := manager.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"},
		manifest); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(door.submitted) < 3 {
		t.Fatalf("expected a change per topic, got %d", len(door.submitted))
	}
	first := door.submitted[0].Params["floorRevision"]
	for _, intent := range door.submitted {
		if got := intent.Params["floorRevision"]; got != first {
			t.Fatalf("one manifest spanned two floors (%v then %v) — the floor is being read "+
				"per change instead of per request", first, got)
		}
	}
}
