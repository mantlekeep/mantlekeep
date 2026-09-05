package estate

import (
	"context"
	"strings"
	"testing"
)

func slotItem(cluster, namespace, name, digest string, gate Gate) DesiredItem {
	return DesiredItem{
		Asset: "app", Kind: "deployment", Name: name,
		Slot: Slot{Cluster: cluster, Namespace: namespace, Name: name},
		Tier: TierProd, Gate: gate, Digest: digest,
		Limits: AppLimits{Replicas: 2, CPULimit: "1", MemoryMiB: 1024},
	}
}

func observedSlot(cluster, namespace, name, digest string, replicas int) ObservedItem {
	return ObservedItem{
		Asset: "app", Kind: "deployment", Name: name,
		Slot:   Slot{Cluster: cluster, Namespace: namespace, Name: name},
		Digest: digest,
		Limits: AppLimits{Replicas: replicas, CPULimit: "1", MemoryMiB: 1024},
	}
}

// Two versions of one app, same cluster, different namespaces — the changeover case. Keying on
// name alone collides them, and the reconciler would overwrite a live version to "fix" it.
func TestTwoVersionsInOneClusterAreTwoSlotsNotDrift(t *testing.T) {
	desired := Desired{Changes: []DesiredItem{
		slotItem("prod", "payments-v1", "api", "sha256:71ab", GateNone),
		slotItem("prod", "payments-v2", "api", "sha256:9c4f", GateNone),
	}}
	observed := Observed{Items: []ObservedItem{
		observedSlot("prod", "payments-v1", "api", "sha256:71ab", 2),
		observedSlot("prod", "payments-v2", "api", "sha256:9c4f", 2),
	}}

	if drifts := DiffOwned(desired, observed, DefaultOwnership()); len(drifts) != 0 {
		t.Fatalf("a side-by-side changeover is not drift; got %+v", drifts)
	}
}

// A slot on an older digest than its neighbour is CORRECT — approval is per slot. Reporting it
// as drift would either force an unscheduled rollout or produce permanent noise.
func TestASlotOnAnOlderDigestIsNotDrift(t *testing.T) {
	desired := Desired{Changes: []DesiredItem{
		slotItem("dev", "payments", "api", "sha256:9c4f", GateNone),
		slotItem("prod", "payments", "api", "sha256:71ab", GatePlatform), // deliberately behind
	}}
	observed := Observed{Items: []ObservedItem{
		observedSlot("dev", "payments", "api", "sha256:9c4f", 2),
		observedSlot("prod", "payments", "api", "sha256:71ab", 2),
	}}

	if drifts := DiffOwned(desired, observed, DefaultOwnership()); len(drifts) != 0 {
		t.Fatalf("a fleet mid-rollout is not drift; got %+v", drifts)
	}
}

// The hand-edit case, and it must name the FIELD.
func TestAChangedDigestIsGovernedDrift(t *testing.T) {
	desired := Desired{Changes: []DesiredItem{slotItem("prod", "payments", "api", "sha256:9c4f", GateNone)}}
	observed := Observed{Items: []ObservedItem{observedSlot("prod", "payments", "api", "sha256:hotfix", 2)}}

	drifts := DiffOwned(desired, observed, DefaultOwnership())
	if len(drifts) != 1 {
		t.Fatalf("want one drift, got %+v", drifts)
	}
	if !drifts[0].Ungoverned() {
		t.Fatal("a changed digest is a violation — MantleKeep governs which artifact runs")
	}
	if len(drifts[0].Differences) != 1 || drifts[0].Differences[0].Field != "digest" {
		t.Fatalf("the drift must name the field that changed; got %+v", drifts[0].Differences)
	}
}

// THE anti-noise rule. The platform autoscales; correcting replicas would fight it on a timer,
// and a report that fires on legitimate autoscaling gets muted within a week.
func TestAutoscaledReplicasAreWatchedNotCorrected(t *testing.T) {
	desired := Desired{Changes: []DesiredItem{slotItem("prod", "payments", "api", "sha256:9c4f", GateNone)}}
	observed := Observed{Items: []ObservedItem{observedSlot("prod", "payments", "api", "sha256:9c4f", 40)}}

	drifts := DiffOwned(desired, observed, DefaultOwnership())
	if len(drifts) != 1 {
		t.Fatalf("a watched difference must still be REPORTED; got %+v", drifts)
	}
	if drifts[0].Ungoverned() {
		t.Fatal("autoscaled replicas were treated as a violation — the reconciler would fight " +
			"the autoscaler forever, and the report would be muted")
	}
	if drifts[0].Correctable() {
		t.Fatal("a watched-only difference must never be auto-corrected")
	}
	if !strings.Contains(drifts[0].Detail, "watched") {
		t.Fatalf("the report must say whose field it is; got %q", drifts[0].Detail)
	}
}

// An approval must bind to an immutable artifact.
func TestAPromotionRefusesATag(t *testing.T) {
	promotion := Promotion{Team: "payments", App: "api", Repository: "harbor/payments/api", Digest: "v1.2.3",
		To: Slot{Cluster: "prod", Namespace: "payments", Name: "api"}, Tier: TierProd}

	err := promotion.Validate()
	if err == nil {
		t.Fatal("a promotion bound to a tag was accepted — the pointer can be moved afterwards")
	}
	if !strings.Contains(err.Error(), "immutable artifact") {
		t.Fatalf("the refusal must say why; got %v", err)
	}
}

// The same digest is a playground change in one slot and a production change in another. The
// artifact did not change; the blast radius did.
func TestTheSameDigestCarriesDifferentGatesPerSlot(t *testing.T) {
	floor := DefaultFloor()
	dev := Promotion{Team: "payments", App: "api", Repository: "harbor/payments/api", Digest: "sha256:9c4f",
		To: Slot{Cluster: "dev", Namespace: "payments", Name: "api"}, Tier: TierDev}
	prod := Promotion{Team: "payments", App: "api", Repository: "harbor/payments/api", Digest: "sha256:9c4f",
		To: Slot{Cluster: "prod", Namespace: "payments", Name: "api"}, Tier: TierProd}

	devChange, err := dev.AsChange(floor, RuntimeEnterprise)
	if err != nil {
		t.Fatalf("dev: %v", err)
	}
	prodChange, err := prod.AsChange(floor, RuntimeEnterprise)
	if err != nil {
		t.Fatalf("prod: %v", err)
	}
	if devChange.Gate != GateNone || prodChange.Gate != GatePlatform {
		t.Fatalf("the gate follows the slot's consequence; got %q and %q",
			devChange.Gate, prodChange.Gate)
	}
	if devChange.Digest != prodChange.Digest {
		t.Fatal("promotion must move the SAME artifact — rebuilding breaks the evidence chain")
	}
}

// fakeRecorder captures discoveries.
type fakeRecorder struct{ found []Discovery }

func (f *fakeRecorder) Record(_ context.Context, d Discovery) error {
	f.found = append(f.found, d)
	return nil
}

// Without this, an out-of-band change that gets auto-corrected leaves NO trace it ever
// happened — the correction looks like routine work.
func TestACorrectedChangeStillLeavesARecord(t *testing.T) {
	door := &fakeDoor{}
	port := &recordingPort{asset: "kafka"}
	recorder := &fakeRecorder{}
	manager := NewManager(door, DefaultFloor(), port).RecordDiscoveriesTo(recorder)

	if _, _, err := manager.Reconcile(context.Background(), actor(), manifestWithProdTopic(t)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(recorder.found) == 0 {
		t.Fatal("drift was handled without being recorded — a silently corrected out-of-band " +
			"change is indistinguishable from routine work")
	}
}
