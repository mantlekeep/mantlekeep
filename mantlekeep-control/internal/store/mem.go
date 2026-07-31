package store

import (
	"context"
	"fmt"
	"strings"
	"sync"

	mantlekeep "mantlekeep.dev/control"
)

// The embedded in-memory driver is ALWAYS available — no build tag, no external
// dependency. It's the default that keeps the bare binary self-contained; real
// database drivers (postgres.go, etc.) are opt-in behind build tags.
func init() {
	Register("mem", func(_ string) (mantlekeep.Store, error) { return NewMem(), nil })
}

// Mem is a concurrency-safe in-memory mantlekeep.Store.
type Mem struct {
	mu   sync.Mutex
	data map[string][]byte
}

// NewMem returns an empty in-memory store.
func NewMem() *Mem { return &Mem{data: map[string][]byte{}} }

// Put implements mantlekeep.Store.
func (m *Mem) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

// Get implements mantlekeep.Store.
func (m *Mem) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	return v, nil
}

// List implements mantlekeep.Store.
func (m *Mem) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}
