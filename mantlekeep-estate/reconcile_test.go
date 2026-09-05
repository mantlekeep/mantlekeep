package estate

import (
	"context"
	"errors"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// testToken is an execution token shaped like a real one: a capability VALUE, the chain
// reference the adapter records, and an expiry, because an adapter is entitled to refuse a
// token that has run out.
func testToken(value string) mantlekeep.ExecutionToken {
	return mantlekeep.ExecutionToken{
		Value: value, IntentID: "ESTATE-" + value, ExpiresAt: time.Now().Add(time.Hour),
	}
}

// fakePort records what it was asked to apply. A fake, not a mock: the assertions are about
// WHICH changes reached the adapter, which is the property under test.
type fakePort struct {
	applied []string
	fail    bool
}

func (f *fakePort) Asset() string                                     { return "kafka" }
func (f *fakePort) Observe(context.Context, string) (Observed, error) { return Observed{}, nil }
func (f *fakePort) Apply(_ context.Context, _ mantlekeep.ExecutionToken, change DesiredItem) error {
	if f.fail {
		return errors.New("broker refused")
	}
	f.applied = append(f.applied, change.Name)
	return nil
}

func devTopic(name string) DesiredItem {
	return DesiredItem{Asset: "kafka", Kind: "topic", Name: name, Tier: TierDev, Gate: GateNone}
}

func prodTopic(name string) DesiredItem {
	return DesiredItem{Asset: "kafka", Kind: "topic", Name: name, Tier: TierProd, Gate: GatePlatform}
}

func TestApprovedButAbsentIsDrift(t *testing.T) {
	drifts := Diff(Desired{Changes: []DesiredItem{devTopic("payments.orders")}}, Observed{})
	if len(drifts) != 1 || drifts[0].Kind != DriftAbsent {
		t.Fatalf("want one absent drift, got %+v", drifts)
	}
}

// The case an apply-once model cannot see at all.
func TestSomethingChangedByHandIsDetected(t *testing.T) {
	desired := Desired{Changes: []DesiredItem{{
		Asset: "app", Kind: "deployment", Name: "payments-api", Gate: GateNone,
		Image: "harbor/payments/api", Runtime: "enterprise",
	}}}
	observed := Observed{Items: []ObservedItem{{
		Asset: "app", Kind: "deployment", Name: "payments-api",
		Image: "harbor/payments/api-hotfix", Runtime: "enterprise",
	}}}

	drifts := Diff(desired, observed)
	if len(drifts) != 1 || drifts[0].Kind != DriftChanged {
		t.Fatalf("want one changed drift, got %+v", drifts)
	}
	if drifts[0].Detail == "" {
		t.Fatal("drift must say WHAT differs — a human decides from the detail, not the flag")
	}
}

// A resource nobody approved is either a bypass or a leftover. Both need a person.
func TestAnUnapprovedResourceIsNeverCorrectedAutomatically(t *testing.T) {
	observed := Observed{Items: []ObservedItem{
		{Asset: "kafka", Kind: "topic", Name: "payments.shadow"},
	}}
	drifts := Diff(Desired{}, observed)

	if len(drifts) != 1 || drifts[0].Kind != DriftUnexpected {
		t.Fatalf("want one unexpected drift, got %+v", drifts)
	}
	if drifts[0].Correctable() {
		t.Fatal("an unapproved resource was marked correctable — deleting data because it is " +
			"unrecognised turns a governance gap into an outage")
	}
}

// THE load-bearing rule. A reconciler that silently re-applies a gated resource is making an
// ungoverned change in the direction of the last approval — still an ungoverned change.
func TestGatedDriftIsEscalatedAndUngatedDriftIsCorrected(t *testing.T) {
	port := &fakePort{}
	drifts := Diff(Desired{Changes: []DesiredItem{
		devTopic("payments.orders"),
		prodTopic("payments.settlements"),
	}}, Observed{})

	outcome := Reconcile(context.Background(), port, testToken("token-123"), drifts)

	if len(outcome.Corrected) != 1 || outcome.Corrected[0].Desired.Name != "payments.orders" {
		t.Fatalf("the ungated dev topic must be corrected automatically; got %+v", outcome.Corrected)
	}
	if len(outcome.Escalated) != 1 || outcome.Escalated[0].Desired.Name != "payments.settlements" {
		t.Fatalf("the gated prod topic must be ESCALATED, never auto-applied; got %+v",
			outcome.Escalated)
	}
	for _, name := range port.applied {
		if name == "payments.settlements" {
			t.Fatal("a gated resource reached the adapter without a human — the reconciler " +
				"made an ungoverned change")
		}
	}
}

func TestAFailedCorrectionIsReportedNotSwallowed(t *testing.T) {
	port := &fakePort{fail: true}
	drifts := Diff(Desired{Changes: []DesiredItem{devTopic("payments.orders")}}, Observed{})

	outcome := Reconcile(context.Background(), port, testToken("token-123"), drifts)

	if len(outcome.Failed) != 1 {
		t.Fatalf("a failed correction must be reported; got %+v", outcome)
	}
	if outcome.Failed[0].Detail == "" {
		t.Fatal("a failure must carry the reason — silence reads as success on the next pass")
	}
}

// Matching reality produces nothing. A reconciler that reports churn on a steady system is a
// reconciler nobody reads.
func TestNoDriftWhenRealityMatches(t *testing.T) {
	item := devTopic("payments.orders")
	observed := Observed{Items: []ObservedItem{{Asset: "kafka", Kind: "topic", Name: "payments.orders"}}}

	if drifts := Diff(Desired{Changes: []DesiredItem{item}}, observed); len(drifts) != 0 {
		t.Fatalf("a matching system must produce no drift; got %+v", drifts)
	}
}

// The most dangerous edit, and it used to be invisible: strip the @sha256 pin off a live
// Deployment and the reference resolves to whatever the registry serves at pull time. compare()
// skipped any field whose observed side was empty, so this produced NO drift — the exact
// moving-pointer attack an approved digest exists to rule out.
func TestRemovingTheDigestPinIsDriftNotSilence(t *testing.T) {
	slot := Slot{Cluster: "prod", Namespace: "payments", Name: "api"}
	desired := Desired{Changes: []DesiredItem{{
		Asset: "app", Kind: "deployment", Name: "api", Slot: slot, Gate: GateNone,
		Image: "harbor/payments/api", Digest: "sha256:9c4f",
	}}}
	observed := Observed{Items: []ObservedItem{{
		Asset: "app", Kind: "deployment", Name: "api", Slot: slot,
		Image: "harbor/payments/api", Digest: "", // the pin was stripped
	}}}

	drifts := DiffOwned(desired, observed, DefaultOwnership())
	if len(drifts) != 1 {
		t.Fatalf("a stripped digest pin must be drift, not silence; got %+v", drifts)
	}
	if !drifts[0].Ungoverned() {
		t.Fatal("the digest is governed — losing the pin is a violation, not a fact about the platform")
	}
	var named bool
	for _, difference := range drifts[0].Differences {
		if difference.Field == "digest" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the drift must name the digest; got %+v", drifts[0].Differences)
	}
}
