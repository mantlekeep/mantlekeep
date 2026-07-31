// Package provider is MantleKeep's Spring-style abstraction for pluggable backends.
// It is the generic mechanism behind "outside tools are swappable": for any
// capability T (a Store, a Forge, a PolicyEvaluator…), you can REGISTER several
// named implementations, keep them ALL LIVE AT ONCE, and let config route each
// purpose to a specific one — like many beans of a type selected by qualifier.
//
// Example: register a Postgres, a MySQL and a MariaDB Store; bind audit→postgres,
// events→mysql, cache→mariadb. All three run together; each purpose uses the one
// config chose, and swapping a backend is a config edit, not a code change.
package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds multiple named implementations of one capability T.
type Registry[T any] struct {
	mu        sync.Mutex
	kind      string
	providers map[string]T
}

// New creates a registry for a capability (kind is used in error messages).
func New[T any](kind string) *Registry[T] {
	return &Registry[T]{kind: kind, providers: map[string]T{}}
}

// Register adds (or replaces) a named provider. Returns the registry for chaining.
func (r *Registry[T]) Register(name string, impl T) *Registry[T] {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = impl
	return r
}

// Get returns the provider registered under name.
func (r *Registry[T]) Get(name string) (T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	impl, ok := r.providers[name]
	if !ok {
		var zero T
		return zero, fmt.Errorf("no %s provider %q registered (have %v)", r.kind, name, r.namesLocked())
	}
	return impl, nil
}

// Names lists registered provider names, sorted.
func (r *Registry[T]) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.namesLocked()
}

func (r *Registry[T]) namesLocked() []string {
	out := make([]string, 0, len(r.providers))
	for n := range r.providers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Bindings maps a logical role → a provider name. Loaded from config (e.g.
// .mantlekeep.yaml) so each purpose can use a different registered provider.
type Bindings map[string]string

// For resolves the provider bound to a role: role → provider name → implementation.
func (r *Registry[T]) For(b Bindings, role string) (T, error) {
	name, ok := b[role]
	if !ok {
		var zero T
		return zero, fmt.Errorf("no %s binding for role %q", r.kind, role)
	}
	return r.Get(name)
}
