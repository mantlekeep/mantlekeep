package estate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// A read-only deployment must refuse even a perfectly valid change: a live token, the right
// asset, resolved limits. Nothing about the request is wrong — the deployment simply cannot
// write, and that is the guarantee.
func TestAReadOnlyDeploymentRefusesAValidChange(t *testing.T) {
	adapter := &recordingAdapter{asset: "app", kinds: []string{"deployment"}}
	port := estate.ReadOnly(estate.Guarded(adapter))

	err := port.Apply(context.Background(), liveToken(), appChange())
	if err == nil {
		t.Fatal("a read-only deployment applied a change")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if len(adapter.applied) != 0 {
		t.Error("the backend was touched by a read-only deployment")
	}
}

// Refusing, never silently succeeding: a nil return would record a change that reality
// never received, which is worse than either applying or refusing.
func TestAReadOnlyApplyIsNeverASilentSuccess(t *testing.T) {
	port := estate.ReadOnly(estate.Guarded(&recordingAdapter{asset: "app"}))
	if err := port.Apply(context.Background(), liveToken(), appChange()); err == nil {
		t.Fatal("a read-only Apply returned success")
	}
}

// Watching is the entire point, so observation must pass straight through.
func TestAReadOnlyDeploymentStillObserves(t *testing.T) {
	adapter := &recordingAdapter{asset: "app"}
	if _, err := estate.ReadOnly(estate.Guarded(adapter)).Observe(context.Background(), "payments"); err != nil {
		t.Fatalf("a read-only deployment could not observe: %v", err)
	}
}

func TestReadOnlyIsAPort(t *testing.T) {
	var _ estate.Port = estate.ReadOnly(estate.Guarded(&recordingAdapter{asset: "app"}))
}
