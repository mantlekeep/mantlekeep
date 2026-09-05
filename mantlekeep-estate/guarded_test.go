package estate_test

import (
	"context"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// recordingAdapter is an adapter written by somebody who checked NOTHING. That is the
// point: every refusal these tests assert has to come from Guarded, because there is no
// other code that could produce it.
type recordingAdapter struct {
	asset   string
	kinds   []string
	applied []estate.DesiredItem
}

func (a *recordingAdapter) Asset() string   { return a.asset }
func (a *recordingAdapter) Kinds() []string { return a.kinds }

func (a *recordingAdapter) Observe(context.Context, string) (estate.Observed, error) {
	return estate.Observed{}, nil
}

func (a *recordingAdapter) ApplyApproved(_ context.Context, _ mantlekeep.ExecutionToken,
	change estate.DesiredItem) error {
	a.applied = append(a.applied, change)
	return nil
}

func liveToken() mantlekeep.ExecutionToken {
	return mantlekeep.ExecutionToken{
		Value:     "opaque-capability",
		IntentID:  "intent-1",
		IssuedAt:  time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func appChange() estate.DesiredItem {
	return estate.DesiredItem{Asset: "app", Kind: "deployment", Name: "payments-api"}
}

// THE test. An adapter that checks nothing still cannot be reached without a token,
// because it does not implement Apply — it cannot be called any other way.
func TestAnAdapterThatChecksNothingStillRefusesAnEmptyToken(t *testing.T) {
	adapter := &recordingAdapter{asset: "app", kinds: []string{"deployment"}}
	port := estate.Guarded(adapter)

	err := port.Apply(context.Background(), mantlekeep.ExecutionToken{}, appChange())
	if err == nil {
		t.Fatal("a change was applied under no execution token")
	}
	if !strings.Contains(err.Error(), "no execution token") {
		t.Errorf("the refusal does not say what was missing: %v", err)
	}
	if len(adapter.applied) != 0 {
		t.Error("the backend was touched despite the refusal")
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	adapter := &recordingAdapter{asset: "app"}
	port := estate.Guarded(adapter)

	expired := liveToken()
	expired.ExpiresAt = time.Now().Add(-time.Second)

	err := port.Apply(context.Background(), expired, appChange())
	if err == nil {
		t.Fatal("a change was applied under an expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("the refusal does not say the token expired: %v", err)
	}
	if len(adapter.applied) != 0 {
		t.Error("the backend was touched despite the refusal")
	}
}

func TestAChangeForAnotherAssetIsRefused(t *testing.T) {
	adapter := &recordingAdapter{asset: "postgres"}
	port := estate.Guarded(adapter)

	err := port.Apply(context.Background(), liveToken(), appChange())
	if err == nil {
		t.Fatal("an adapter applied a change belonging to another asset")
	}
	if !strings.Contains(err.Error(), "not this adapter's concern") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

func TestAChangeOfAnUnhandledKindIsRefused(t *testing.T) {
	adapter := &recordingAdapter{asset: "app", kinds: []string{"deployment"}}
	port := estate.Guarded(adapter)

	change := appChange()
	change.Kind = "statefulset"

	if err := port.Apply(context.Background(), liveToken(), change); err == nil {
		t.Fatal("an adapter applied a kind it does not handle")
	}
}

// An adapter that declares no kinds handles every kind of its asset. Refusing here would
// force a list on adapters whose backend has only one kind of thing in it.
func TestAnAdapterDeclaringNoKindsHandlesThemAll(t *testing.T) {
	adapter := &recordingAdapter{asset: "app"}
	port := estate.Guarded(adapter)

	change := appChange()
	change.Kind = "anything"

	if err := port.Apply(context.Background(), liveToken(), change); err != nil {
		t.Fatalf("an adapter declaring no kinds refused one: %v", err)
	}
	if len(adapter.applied) != 1 {
		t.Error("the change did not reach the backend")
	}
}

// The guards must not become a wall. A change that passes them all must arrive intact.
func TestAnApprovedChangeReachesTheBackendUnchanged(t *testing.T) {
	adapter := &recordingAdapter{asset: "app", kinds: []string{"deployment"}}
	port := estate.Guarded(adapter)

	if err := port.Apply(context.Background(), liveToken(), appChange()); err != nil {
		t.Fatalf("an approved change was refused: %v", err)
	}
	if len(adapter.applied) != 1 || adapter.applied[0].Name != "payments-api" {
		t.Errorf("the change did not arrive as sent: %+v", adapter.applied)
	}
}

// Guarded must satisfy Port, or it cannot be registered where an adapter goes.
func TestGuardedIsAPort(t *testing.T) {
	var _ estate.Port = estate.Guarded(&recordingAdapter{asset: "app"})
}

// Observation carries no token and routes no change, so it must pass straight through
// rather than acquire refusals that would make a read fail for governance reasons.
func TestObserveIsNotGuarded(t *testing.T) {
	adapter := &recordingAdapter{asset: "app"}
	if _, err := estate.Guarded(adapter).Observe(context.Background(), "payments"); err != nil {
		t.Fatalf("observation was refused: %v", err)
	}
}
