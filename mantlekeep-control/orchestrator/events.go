package orchestrator

import (
	"context"
	"sync"
	"time"
)

// EventKind is a workflow lifecycle transition. The event trail is the runtime
// counterpart to the audit chain: audit records the decision at the door, the
// event store records what the spine actually did.
type EventKind string

const (
	RunStarted      EventKind = "run.started"
	StepStarted     EventKind = "step.started"
	StepSucceeded   EventKind = "step.succeeded"
	StepFailed      EventKind = "step.failed"
	StepCompensated EventKind = "step.compensated"
	RunFailed       EventKind = "run.failed"
	RunSucceeded    EventKind = "run.succeeded"
)

// Event is one immutable entry in a run's timeline. JSON tags are lowercase so
// the Portal API and its PWA share one wire shape.
type Event struct {
	Seq    int       `json:"seq"`    // monotonic within the store
	At     time.Time `json:"at"`     // set by the engine
	Run    string    `json:"run"`    // DAG name
	Step   string    `json:"step"`   // step name ("" for run-level events)
	Kind   EventKind `json:"kind"`   //
	Detail string    `json:"detail"` // human-readable note (reason, artifact, error)
	// Chain is the audit chain head hash this saga record correlates to, when the store is
	// wired with a correlation source. It ties the (off-chain) saga timeline to the
	// tamper-evident chain without putting the timeline on the chain. Empty when uncorrelated.
	Chain string `json:"chain,omitempty"`
}

// EventStore is the append-only sink for workflow events. Impl: in-memory (MVP)
// → NATS JetStream / bbolt (durable, distributed). The Engine depends only on
// this interface.
type EventStore interface {
	Append(ctx context.Context, e Event) (Event, error)
	Events(ctx context.Context, run string) ([]Event, error)
}

// MemStore is a concurrency-safe in-memory EventStore for embedded runs and tests.
type MemStore struct {
	mu  sync.Mutex
	seq int
	all []Event
}

// NewMemStore returns an empty in-memory event store.
func NewMemStore() *MemStore { return &MemStore{} }

// Append stamps a monotonic Seq and stores the event.
func (m *MemStore) Append(_ context.Context, e Event) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	e.Seq = m.seq
	m.all = append(m.all, e)
	return e, nil
}

// Events returns the events for a run in insertion order (all runs if run == "").
func (m *MemStore) Events(_ context.Context, run string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, 0, len(m.all))
	for _, e := range m.all {
		if run == "" || e.Run == run {
			out = append(out, e)
		}
	}
	return out, nil
}
