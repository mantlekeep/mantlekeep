package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/store"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/orchestrator"
)

// The durable EventStore must survive a restart: append a run's timeline, CLOSE the
// store (process exits), REOPEN the same file (process restarts), and the history is
// still there in Seq order. This is exactly "close the laptop, come back, resume".
func TestBoltEventsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.db")

	es, err := store.OpenBoltEvents(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, k := range []orchestrator.EventKind{orchestrator.RunStarted, orchestrator.StepSucceeded, orchestrator.RunSucceeded} {
		if _, err := es.Append(ctx, orchestrator.Event{Run: "xva", Kind: k}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// A second run, interleaved, to prove per-run filtering survives too.
	if _, err := es.Append(ctx, orchestrator.Event{Run: "other", Kind: orchestrator.RunStarted}); err != nil {
		t.Fatalf("append other: %v", err)
	}
	if err := es.Close(); err != nil { // the "restart" boundary
		t.Fatalf("close: %v", err)
	}

	reopened, err := store.OpenBoltEvents(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	evs, err := reopened.Events(ctx, "xva")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("run history lost across restart: got %d events, want 3", len(evs))
	}
	// Seq must be monotonic and in insertion order.
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq <= evs[i-1].Seq {
			t.Fatalf("Seq not monotonic after reopen: %d then %d", evs[i-1].Seq, evs[i].Seq)
		}
	}
	if evs[0].Kind != orchestrator.RunStarted || evs[2].Kind != orchestrator.RunSucceeded {
		t.Fatalf("timeline order wrong after reopen: %v", evs)
	}

	// A brand-new Append after reopen must continue the sequence, not reset it.
	got, err := reopened.Append(ctx, orchestrator.Event{Run: "xva", Kind: orchestrator.StepStarted})
	if err != nil {
		t.Fatalf("append after reopen: %v", err)
	}
	if got.Seq <= evs[len(evs)-1].Seq {
		t.Fatalf("Seq reset across restart: new %d not after %d", got.Seq, evs[len(evs)-1].Seq)
	}
}
