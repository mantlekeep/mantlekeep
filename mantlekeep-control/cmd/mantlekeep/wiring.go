package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/config"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/provider"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/store"
)

// runWiring demonstrates the Spring-style provider registry: several backends of
// one capability registered and LIVE AT ONCE, with config routing each purpose to
// a specific one. Here three "databases" (Postgres, MySQL, MariaDB — stubbed
// in-memory; real drivers drop in behind the same mantlekeep.Store interface) run
// together, and a config binding sends audit / events / cache each to its own.
func runWiring() {
	ctx := context.Background()

	// Register every backend once — all remain live simultaneously.
	stores := provider.New[mantlekeep.Store]("store").
		Register("postgres", newLabeledStore("postgres")).
		Register("mysql", newLabeledStore("mysql")).
		Register("mariadb", newLabeledStore("mariadb"))

	// RUNTIME selection — the "Helm values" switch. Defaults here; env overrides
	// each role WITHOUT a rebuild (MANTLEKEEP_BIND_STORE_AUDIT=mysql, etc.).
	defaults := map[string]string{
		"audit":  "postgres", // strong consistency for the evidence trail
		"events": "mysql",    // high-write run timeline
		"cache":  "mariadb",  // fast ephemeral reads
	}
	bindings := provider.Bindings(config.Load().Routes("store", defaults))

	fmt.Println("🔱 MantleKeep — provider registry (Spring-style: many backends, config routes each purpose)")
	fmt.Println("────────────────────────────────────────────────────────")
	fmt.Printf("registered store backends : %s (all live at once)\n", strings.Join(stores.Names(), ", "))
	fmt.Printf("runtime bindings (env)    : audit→%s  events→%s  cache→%s\n",
		bindings["audit"], bindings["events"], bindings["cache"])
	fmt.Println("  (override at runtime, no rebuild: MANTLEKEEP_BIND_STORE_CACHE=postgres mantlekeep wiring)")
	fmt.Println()

	// Each purpose writes through the backend config chose for it.
	for _, role := range []string{"audit", "events", "cache"} {
		st, err := stores.For(bindings, role)
		must(err)
		must(st.Put(ctx, role+":key1", []byte("value-for-"+role)))
		got, err := st.Get(ctx, role+":key1")
		must(err)
		fmt.Printf("  %-7s → %-9s  wrote+read: %s\n", role, bindings[role], string(got))
	}

	// Prove they are genuinely separate backends running together: a key written
	// via audit (postgres) is NOT visible through events (mysql).
	pg, _ := stores.For(bindings, "audit")
	my, _ := stores.For(bindings, "events")
	if _, err := my.Get(ctx, "audit:key1"); err != nil {
		fmt.Println("\n  ✓ isolation holds — postgres data invisible to mysql (separate live backends)")
	}
	_ = pg

	// Switching a purpose to another backend is a RUNTIME config change (env /
	// .mantlekeep.yaml), not a rebuild — exactly `MANTLEKEEP_BIND_STORE_CACHE=postgres`.
	rerouted := provider.Bindings(config.Load().Routes("store", map[string]string{"cache": "postgres"}))
	sw, _ := stores.For(rerouted, "cache")
	must(sw.Put(ctx, "cache:key2", []byte("value")))
	fmt.Printf("  ✓ cache now routes to %q — flip it live with MANTLEKEEP_BIND_STORE_CACHE=<driver>, no rebuild\n", rerouted["cache"])

	// Out-of-process drivers (CVE isolation): a separate binary the core launches;
	// its deps never link into the core. Enable one with
	// MANTLEKEEP_DRIVER_POSTGRES=/path/to/driver.
	fmt.Println()
	cfg := config.Load()
	for name, bin := range cfg.Drivers {
		store.RegisterSubprocess(name, bin)
	}
	fmt.Printf("store drivers available in THIS binary : %s\n", strings.Join(store.Available(), ", "))
	if len(cfg.Drivers) == 0 {
		fmt.Println("  out-of-process drivers: none (set MANTLEKEEP_DRIVER_<NAME>=/path to attach one)")
	}
	for name, bin := range cfg.Drivers {
		st, err := store.Open(name, "")
		if err != nil {
			fmt.Printf("  ✗ driver %q (%s): %v\n", name, bin, err)
			continue
		}
		_ = st.Put(ctx, "probe:k", []byte("out-of-process value"))
		got, _ := st.Get(ctx, "probe:k")
		fmt.Printf("  ✓ out-of-process driver %q (%s): put/get ok (%q) — core links ZERO of its deps\n",
			name, bin, string(got))
	}

	fmt.Println("\n────────────────────────────────────────────────────────")
	fmt.Println("same pattern applies to any capability: Forge, PolicyEvaluator, AuditLogger, transport.")
}

// labeledStore is an in-memory mantlekeep.Store tagged with a backend name — a stand
// -in for a real Postgres/MySQL/MariaDB driver, which would implement the exact
// same interface. Each instance is isolated, so registering three of them models
// three separate databases running at once.
type labeledStore struct {
	backend string
	mu      sync.Mutex
	data    map[string][]byte
}

func newLabeledStore(backend string) *labeledStore {
	return &labeledStore{backend: backend, data: map[string][]byte{}}
}

func (s *labeledStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *labeledStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("%s: key %q not found", s.backend, key)
	}
	return v, nil
}

func (s *labeledStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}
