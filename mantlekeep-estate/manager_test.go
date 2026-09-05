package estate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// fakeDoor records what it was asked to allow, and refuses whatever the test names. Denial is
// an error carrying the decision's own words, exactly as the real door reports it.
type fakeDoor struct {
	submitted []mantlekeep.Intent
	denyTier  Tier
}

func (d *fakeDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.submitted = append(d.submitted, intent)
	if d.denyTier != "" && intent.Params["tier"] == string(d.denyTier) {
		return mantlekeep.ExecutionToken{}, errors.New("require_approval: a platform approver must sign off")
	}
	return mantlekeep.ExecutionToken{
		Value: "tok-" + intent.ID, IntentID: intent.ID,
		ExpiresAt: time.Now().Add(time.Minute),
	}, nil
}

// recordingPort remembers the tokens it was handed — the property under test is that a real
// one arrives, not a placeholder.
type recordingPort struct {
	asset   string
	tokens  map[string]string
	intents map[string]string
	observe Observed
	failOn  string
}

func (p *recordingPort) Asset() string { return p.asset }

func (p *recordingPort) Observe(context.Context, string) (Observed, error) { return p.observe, nil }

// Apply records the token's VALUE — the capability — because that is what proves the door
// minted it. What an adapter may PUBLISH is a different question, answered by the adapter that
// publishes anything (see the Kubernetes adapter).
func (p *recordingPort) Apply(_ context.Context, token mantlekeep.ExecutionToken, change DesiredItem) error {
	if change.Name == p.failOn {
		return errors.New("the cluster refused it")
	}
	if p.tokens == nil {
		p.tokens = map[string]string{}
	}
	if p.intents == nil {
		p.intents = map[string]string{}
	}
	p.tokens[change.Name] = token.Value
	p.intents[change.Name] = token.IntentID
	return nil
}

func manifestWithProdTopic(t *testing.T) Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "kafka":{"topics":["orders",{"name":"settlements","tier":"prod"}]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

func actor() mantlekeep.Subject { return mantlekeep.Subject{ID: "dev-alice"} }

// The adapter must receive a token the DOOR minted. Anything else means a change reached the
// asset without a decision behind it.
func TestTheAdapterReceivesTheDoorsToken(t *testing.T) {
	door := &fakeDoor{}
	port := &recordingPort{asset: "kafka"}
	manager := NewManager(door, DefaultFloor(), port)

	outcome, err := manager.Apply(context.Background(), actor(), manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Applied) == 0 {
		t.Fatalf("nothing was applied: %+v", outcome)
	}
	for name, token := range port.tokens {
		if !strings.HasPrefix(token, "tok-") {
			t.Fatalf("%s was applied with %q, which the door did not mint", name, token)
		}
	}
	// And the CHAIN REFERENCE arrives with it. An adapter that records anything about the
	// approval an object exists under needs the intent id; without it in hand the only string
	// it could write was the capability, which is how a live token reached a Kubernetes
	// object anyone with read access can see.
	for name, intentID := range port.intents {
		if intentID == "" {
			t.Fatalf("%s was applied with no intent id — the adapter has nothing to record "+
				"except the capability it was supposed to keep", name)
		}
	}
}

// THE rule: govern BEFORE execute. A refused change must never reach the adapter.
func TestARefusedChangeNeverReachesTheAsset(t *testing.T) {
	door := &fakeDoor{denyTier: TierProd}
	port := &recordingPort{asset: "kafka"}
	manager := NewManager(door, DefaultFloor(), port)

	outcome, err := manager.Apply(context.Background(), actor(), manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if _, reached := port.tokens["payments.settlements"]; reached {
		t.Fatal("a refused change reached the adapter — the door was consulted after the fact")
	}
	if len(outcome.Refused) != 1 {
		t.Fatalf("want one refusal, got %+v", outcome.Refused)
	}
	// The door's own words, not ours: two sentences for one decision means the person acts on
	// the wrong one.
	if !strings.Contains(outcome.Refused[0].Refused, "require_approval") {
		t.Fatalf("the refusal must carry the door's reason; got %q", outcome.Refused[0].Refused)
	}
}

// Blocking every dev resource because one prod resource awaits approval is over-gating by
// another route.
func TestOneRefusalDoesNotBlockTheRest(t *testing.T) {
	door := &fakeDoor{denyTier: TierProd}
	port := &recordingPort{asset: "kafka"}
	manager := NewManager(door, DefaultFloor(), port)

	outcome, err := manager.Apply(context.Background(), actor(), manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, reached := port.tokens["payments.orders"]; !reached {
		t.Fatal("the dev topic was blocked by an unrelated refusal")
	}
	if len(outcome.Applied) == 0 {
		t.Fatal("nothing applied — a gate on one item stopped the others")
	}
}

// The door must be able to rule on CONSEQUENCE without knowing what a Kafka topic is.
func TestTheIntentCarriesTierAndGateSoPolicyNeedNotKnowTheAsset(t *testing.T) {
	door := &fakeDoor{}
	manager := NewManager(door, DefaultFloor(), &recordingPort{asset: "kafka"})

	if _, err := manager.Apply(context.Background(), actor(), manifestWithProdTopic(t)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var sawProd bool
	for _, intent := range door.submitted {
		if intent.Action != "estate.apply" {
			t.Fatalf("unexpected action %q", intent.Action)
		}
		if intent.Spec.Goal == "" {
			t.Fatal("an intent with no goal would be rejected by the door")
		}
		if intent.Params["tier"] == string(TierProd) {
			sawProd = true
			if intent.Params["gate"] != string(GatePlatform) {
				t.Fatalf("a prod change must carry the platform gate; got %v", intent.Params["gate"])
			}
		}
	}
	if !sawProd {
		t.Fatal("the prod topic was never submitted")
	}
}

// An approval for work that never happened is worse than a refusal: the record says it was fine.
func TestAnAssetWithNoAdapterIsReportedNotSilentlyApproved(t *testing.T) {
	door := &fakeDoor{}
	manager := NewManager(door, DefaultFloor()) // no ports at all

	outcome, err := manager.Apply(context.Background(), actor(), manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Failed) == 0 {
		t.Fatal("a change with no adapter was not reported")
	}
	if len(door.submitted) != 0 {
		t.Fatal("an intent was submitted for work nothing could execute — the chain would " +
			"record an approval for something that never happened")
	}
}

// Correcting drift is itself a change, so it passes the door like any other.
func TestReconcileGovernsEveryCorrection(t *testing.T) {
	door := &fakeDoor{}
	port := &recordingPort{asset: "kafka"} // observes nothing: everything is absent
	manager := NewManager(door, DefaultFloor(), port)

	outcome, escalated, err := manager.Reconcile(context.Background(), actor(), manifestWithProdTopic(t))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(door.submitted) == 0 {
		t.Fatal("a correction was applied without passing the door — a reconciler that " +
			"corrects without governing makes ungoverned changes on a timer")
	}
	if len(outcome.Applied) == 0 {
		t.Fatal("no ungated drift was corrected")
	}
	// The prod topic drifted too, and must be escalated rather than re-applied.
	var sawProd bool
	for _, drift := range escalated {
		if drift.Desired != nil && drift.Desired.Name == "payments.settlements" {
			sawProd = true
		}
	}
	if !sawProd {
		t.Fatal("gated drift must be escalated, never silently corrected")
	}
}
