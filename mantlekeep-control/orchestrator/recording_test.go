package orchestrator

import (
	"context"
	"sync"
	"testing"

	mantlekeep "mantlekeep.dev/control"
)

// okRunner is a StepRunner whose steps all succeed. Enough to exercise the timeline.
type okRunner struct{}

func (okRunner) Run(_ context.Context, s mantlekeep.Step) (string, error) {
	return "ran " + s.Name, nil
}
func (okRunner) Compensate(_ context.Context, s mantlekeep.Step) (string, error) {
	return "undid " + s.Name, nil
}

func twoStepDAG() mantlekeep.DAG {
	return mantlekeep.DAG{Name: "run-a", Steps: []mantlekeep.Step{
		{Name: "first"},
		{Name: "second", DependsOn: []string{"first"}},
	}}
}

func runAtLevel(t *testing.T, level RecordingLevel) int {
	t.Helper()
	events := NewMemStore()
	engine := NewEngine(okRunner{}, events).WithRecording(level)
	if _, err := engine.Run(context.Background(), validToken(), twoStepDAG()); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	all, _ := events.Events(context.Background(), "")
	return len(all)
}

func TestRecordingLevelGatesTheTimelineButNeverGovernance(t *testing.T) {
	// steps/full keep the per-step timeline; none/decisions keep none. The run still
	// SUCCEEDS at every level — recording changes what is kept, not what is governed.
	stepsCount := runAtLevel(t, RecordSteps)
	if stepsCount == 0 {
		t.Fatal("RecordSteps must keep the per-step timeline")
	}
	if got := runAtLevel(t, RecordFull); got != stepsCount {
		t.Errorf("RecordFull should keep the same timeline as RecordSteps in the core engine, got %d want %d", got, stepsCount)
	}
	if got := runAtLevel(t, RecordNone); got != 0 {
		t.Errorf("RecordNone must keep no timeline, got %d events", got)
	}
	if got := runAtLevel(t, RecordDecisions); got != 0 {
		t.Errorf("RecordDecisions must keep no timeline (chain only), got %d events", got)
	}
}

func TestDefaultLevelKeepsTheTimeline(t *testing.T) {
	// A caller that never sets a level (NewEngine alone) must behave as before — emit.
	events := NewMemStore()
	engine := NewEngine(okRunner{}, events)
	if _, err := engine.Run(context.Background(), validToken(), twoStepDAG()); err != nil {
		t.Fatal(err)
	}
	all, _ := events.Events(context.Background(), "")
	if len(all) == 0 {
		t.Fatal("the default engine must keep the timeline (backward compatible)")
	}
}

// memKV is a concurrency-safe in-memory mantlekeep.Store for exercising StoreEvents.
type memKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemKV() *memKV { return &memKV{m: map[string][]byte{}} }

func (k *memKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}
func (k *memKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	stored := make([]byte, len(value))
	copy(stored, value)
	k.m[key] = stored
	return nil
}
func (k *memKV) List(_ context.Context, prefix string) ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var keys []string
	for key := range k.m {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func TestStoreEventsPersistAndCorrelateToTheChain(t *testing.T) {
	kv := newMemKV()
	store := NewStoreEvents(kv, "saga").WithCorrelation(func() string { return "CHAINHEAD" })
	engine := NewEngine(okRunner{}, store)

	if _, err := engine.Run(context.Background(), validToken(), twoStepDAG()); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	events, err := store.Events(context.Background(), "run-a")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("store-backed timeline kept nothing")
	}
	// Seq is monotonic and ordered.
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("events out of order at %d: %d then %d", i, events[i-1].Seq, events[i].Seq)
		}
	}
	// Every record is correlated to the chain head it was written under.
	for _, e := range events {
		if e.Chain != "CHAINHEAD" {
			t.Errorf("event %q not correlated to the chain head, got %q", e.Kind, e.Chain)
		}
	}
}
