package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	mantlekeep "mantlekeep.dev/control"
)

// counter is a concurrency-safe monotonic sequence. The engine emits from per-step
// goroutines, so Append must hand out sequence numbers without a race.
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) next() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}

// StoreEvents is an EventStore that persists the saga timeline through the generic
// StorePort (mantlekeep.Store), so a deployment binds it to whatever backend it already
// trusts — bbolt embedded, a database adapter — WITHOUT the orchestrator knowing which.
// This is the durable counterpart to MemStore: the same timeline, kept.
//
// It optionally correlates each record to the audit chain head (WithCorrelation), tying
// the off-chain timeline to the tamper-evident chain. The timeline is deliberately NOT on
// the chain — it is high-volume and per-run — but a reader can prove which chain state each
// record was written under.
//
// Keys are `<prefix>/<run>/<zero-padded seq>` so a prefix List returns a run's events in
// order without a separate index.
type StoreEvents struct {
	store     mantlekeep.Store
	prefix    string
	correlate func() string // chain head at write time; nil = no correlation
	seq       *counter
}

// NewStoreEvents binds a StorePort as the durable saga timeline under the given key prefix
// (e.g. "saga"). The prefix keeps the timeline in its own keyspace, separate from any other
// use of the same store.
func NewStoreEvents(store mantlekeep.Store, prefix string) *StoreEvents {
	return &StoreEvents{
		store:  store,
		prefix: strings.Trim(prefix, "/"),
		seq:    &counter{},
	}
}

// WithCorrelation wires a source of the current audit chain head. Each appended event then
// records that head, tying the saga timeline to the chain. Passing nil (the default) leaves
// records uncorrelated. Returns the receiver to chain.
func (s *StoreEvents) WithCorrelation(chainHead func() string) *StoreEvents {
	s.correlate = chainHead
	return s
}

// Append stamps a monotonic Seq (and the chain head, if correlated) and persists the event.
func (s *StoreEvents) Append(ctx context.Context, e Event) (Event, error) {
	e.Seq = s.seq.next()
	if s.correlate != nil {
		e.Chain = s.correlate()
	}
	encoded, err := json.Marshal(e)
	if err != nil {
		return Event{}, fmt.Errorf("saga event marshal: %w", err)
	}
	if err := s.store.Put(ctx, s.key(e.Run, e.Seq), encoded); err != nil {
		return Event{}, fmt.Errorf("saga event persist: %w", err)
	}
	return e, nil
}

// Events returns a run's events in Seq order (all runs if run == "").
func (s *StoreEvents) Events(ctx context.Context, run string) ([]Event, error) {
	prefix := s.prefix + "/"
	if run != "" {
		prefix += run + "/"
	}
	keys, err := s.store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("saga event list: %w", err)
	}
	events := make([]Event, 0, len(keys))
	for _, key := range keys {
		raw, err := s.store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("saga event read %q: %w", key, err)
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("saga event decode %q: %w", key, err)
		}
		events = append(events, e)
	}
	// List order is backend-defined; sort by the monotonic Seq so the timeline is stable.
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	return events, nil
}

// key is `<prefix>/<run>/<zero-padded seq>`. Zero-padding keeps lexical order == Seq order,
// so even a store that Lists lexically returns events sorted.
func (s *StoreEvents) key(run string, seq int) string {
	return fmt.Sprintf("%s/%s/%010d", s.prefix, run, seq)
}
