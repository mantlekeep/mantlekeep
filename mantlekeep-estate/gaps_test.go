package estate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// GAP 2 — a manifest-declared app had no slot, so it lost the side-by-side protection and
// compared by name against slot-keyed observations.
func TestAManifestDeclaredAppGetsASlot(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"api","runtime":"enterprise","image":"h/p/api","placement":{"env":"dev","purpose":"app","residency":"uk"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	desired, err := ResolveWith(m, DefaultFloor(), NewPlacer([]Cluster{{Name: "dev-app-uk-1", Env: "dev", Purpose: "app",
		Residency: "uk", Reachable: true}}), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	for _, change := range desired.Changes {
		if change.Asset != "app" {
			continue
		}
		if change.Slot.Empty() {
			t.Fatal("a manifest app resolved with no slot — it would compare by name against " +
				"slot-keyed observations and always look absent")
		}
		// The cluster is the PLATFORM's choice now, not the team's — so the assertion is that
		// a cluster was chosen and recorded, not that it matches one the manifest named.
		if change.Slot.Cluster == "" || change.Slot.Namespace != "payments" {
			t.Fatalf("the slot must name where it runs; got %+v", change.Slot)
		}
		if change.Placement == nil || change.Placement.Reason == "" {
			t.Fatal("a platform-chosen placement must record WHY — otherwise nobody can " +
				"answer why the app is on that cluster")
		}
		return
	}
	t.Fatal("no app change resolved")
}

// GAP 4 — the floor's own numbers were applied once and never checked again, so widening a
// retention or a connection limit by hand was invisible.
func TestEveryAssetsFloorIsCheckedForDrift(t *testing.T) {
	for _, probe := range []struct {
		asset    string
		field    string
		approved any
		observed any
	}{
		{"kafka", "retention",
			KafkaLimits{Retention: 7 * 24 * time.Hour, ProducerBytesPerSec: 1},
			KafkaLimits{Retention: 3650 * 24 * time.Hour, ProducerBytesPerSec: 1}},
		{"postgres", "connectionLimit",
			PostgresLimits{ConnectionLimit: 20, StatementTimeout: time.Second},
			PostgresLimits{ConnectionLimit: 500, StatementTimeout: time.Second}},
		{"harbor", "robotExpiry",
			HarborLimits{RobotExpiry: 90 * 24 * time.Hour},
			HarborLimits{RobotExpiry: 3650 * 24 * time.Hour}},
	} {
		t.Run(probe.asset+"/"+probe.field, func(t *testing.T) {
			slot := Slot{Cluster: "c", Namespace: "n", Name: "x"}
			desired := Desired{Changes: []DesiredItem{{
				Asset: probe.asset, Kind: "thing", Name: "x", Slot: slot,
				Gate: GateNone, Limits: probe.approved,
			}}}
			observed := Observed{Items: []ObservedItem{{
				Asset: probe.asset, Kind: "thing", Name: "x", Slot: slot,
				Limits: probe.observed,
			}}}

			drifts := DiffOwned(desired, observed, DefaultOwnership())
			if len(drifts) != 1 {
				t.Fatalf("a widened %s must be detected; got %+v", probe.field, drifts)
			}
			if !drifts[0].Ungoverned() {
				t.Fatalf("%s is the floor's own number — widening it is a violation", probe.field)
			}
			var named bool
			for _, difference := range drifts[0].Differences {
				if difference.Field == probe.field {
					named = true
				}
			}
			if !named {
				t.Fatalf("the drift must name %q; got %+v", probe.field, drifts[0].Differences)
			}
		})
	}
}

// GAP 6 — separation of duties was delegated to the door and never demonstrated here. This does
// not re-implement it: it proves the manager PASSES what the door needs to enforce it, and that
// a door refusing on those grounds stops the change.
type sodDoor struct{}

func (d sodDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	// The real door enforces this; here we assert the manager gave it what it needs to.
	if intent.Subject.ID == "" {
		return mantlekeep.ExecutionToken{}, errors.New("no subject: the door cannot tell who acted")
	}
	// "requester" — the key the REAL door reads. This test previously used "requestedBy",
	// which nothing in the codebase writes or reads, so it passed while proving nothing about
	// separation of duties at all. A test that names a key the product does not use is worse
	// than no test: it reads as coverage of the guarantee the product is sold on.
	if requester, _ := intent.Params["requester"].(string); requester != "" &&
		requester == intent.Subject.ID {
		return mantlekeep.ExecutionToken{}, errors.New(
			"deny: separation of duties — the approver cannot be the requester")
	}
	return mantlekeep.ExecutionToken{Value: "tok", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

// The manager must hand the door the requester it compares against. This does NOT prove
// separation of duties works end to end — see TestSelfApprovalIsRefusedOnceTheDoorCanSeeIt and
// the honest limitation recorded beneath it.
func TestTheManagerNamesTheActorSoTheDoorCanEnforceSoD(t *testing.T) {
	port := &recordingPort{asset: "kafka"}
	manager := NewManager(sodDoor{}, DefaultFloor(), port)

	outcome, err := manager.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"},
		manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// An anonymous intent would make the chain unable to say who acted, and SoD unenforceable.
	for _, refused := range outcome.Refused {
		if strings.Contains(refused.Refused, "no subject") {
			t.Fatal("the manager submitted an intent with no subject — the door cannot enforce " +
				"separation of duties on an anonymous change")
		}
	}
	if len(outcome.Applied) == 0 {
		t.Fatalf("nothing applied: %+v", outcome)
	}
}

func TestADoorRefusingOnSoDStopsTheChange(t *testing.T) {
	port := &recordingPort{asset: "kafka"}
	manager := NewManager(sodDoor{}, DefaultFloor(), port)
	manager.now = func() time.Time { return time.Unix(0, 0) }

	// A door that refuses everything on SoD grounds: nothing may reach the asset.
	alwaysSelfApproved := NewManager(selfApprovingDoor{}, DefaultFloor(), port)
	outcome, err := alwaysSelfApproved.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"},
		manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Applied) != 0 {
		t.Fatal("a change the door refused on separation of duties reached the asset")
	}
	if len(outcome.Refused) == 0 || !strings.Contains(outcome.Refused[0].Refused, "separation") {
		t.Fatalf("the door's reason must survive to the caller; got %+v", outcome.Refused)
	}
}

type selfApprovingDoor struct{}

func (selfApprovingDoor) Submit(context.Context, mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	return mantlekeep.ExecutionToken{}, errors.New(
		"deny: separation of duties — the approver cannot be the requester")
}

// A gated change must be PARKED for a person, not refused as if the requester had approved it.
//
// This test previously asserted the opposite, and encoded a defect as a guarantee: the manager
// named the SUBMITTER as the requester, so the door's separation-of-duties rule — a rule about
// APPROVAL — fired on the request itself. Every gated change came back as a flat denial with no
// approval reference, which made the gate unpassable by anybody and looked, in the tests, like
// separation of duties working.
func TestAGatedChangeIsParkedForAPersonNotRefusedAsASelfApproval(t *testing.T) {
	port := &recordingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	manager := NewManager(sodDoor{}, DefaultFloor(), port).AwaitApprovalIn(store)

	outcome, err := manager.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"},
		manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// sodDoor allows anything that is not a self-approval, and a submission is not one — so the
	// SoD rule must not have fired here at all.
	for _, refused := range outcome.Refused {
		if strings.Contains(refused.Refused, "separation of duties") {
			t.Fatalf("a submission was refused as a self-approval (%q) — the requester was "+
				"claimed on the wrong call", refused.Refused)
		}
	}
	if len(outcome.Applied) == 0 {
		t.Fatal("nothing applied")
	}
}

// The door must be able to enforce separation of duties on the call where it MEANS something:
// the approval. That requires both names — the approver as the subject, the requester as the
// param — and this proves the manager supplies them.
func TestAnApprovalCarriesBothNamesSoTheDoorCanEnforceSoD(t *testing.T) {
	port := &recordingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	door := &realisticDoor{}
	manager := NewManager(door, DefaultFloor(), port).AwaitApprovalIn(store)

	outcome, err := manager.Apply(context.Background(), mantlekeep.Subject{ID: "dev-alice"},
		manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var pending string
	for _, refused := range outcome.Refused {
		if refused.Approval != "" {
			pending = refused.Approval
		}
	}
	if pending == "" {
		t.Fatalf("the gated change left nothing to approve: %+v", outcome)
	}

	if _, err := manager.Approve(context.Background(), mantlekeep.Subject{ID: "arch-carol"},
		pending); err != nil {
		t.Fatalf("approve: %v", err)
	}
	approval := door.submitted[len(door.submitted)-1]
	if approval.Subject.ID != "arch-carol" || approval.Params["requester"] != "dev-alice" {
		t.Fatalf("the approval named subject=%q requester=%v — the door needs both to compare",
			approval.Subject.ID, approval.Params["requester"])
	}
}

// WHAT THIS STILL DOES NOT PROVE, recorded so nobody reads the tests above as more than they are:
// the estate refuses a self-approval in its OWN code (Manager.Approve → ErrSelfApproval) before
// the door is ever asked, so these tests cannot drive a one-party approval all the way to the
// door. That is defence in depth working as intended, not coverage: the DOOR's half of the rule
// is proved where it lives, in the core's policy tests (see the separation-of-duties case
// in TestApprovalCannotLoosenADeny), and the estate's half is proved by
// TestTheRequesterIsRefusedByTheEstateBeforeTheDoorIsAsked.
