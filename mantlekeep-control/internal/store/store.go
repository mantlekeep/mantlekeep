// Package store is the driver registry for persistence backends — and the
// pattern for keeping the core clean of heavy dependencies. Each real driver
// (Postgres, MySQL, …) lives in its OWN file behind a build tag and registers
// itself from init(). The default binary compiles none of them, so their heavy
// libraries (pgx, mysql, …) are never linked — no bloat, no CVE surface, nothing
// to break the core. You opt a driver in at BUILD time:
//
//	go build                       # only the embedded 'mem' driver
//	go build -tags "postgres"      # + the Postgres driver (pulls pgx)
//	go build -tags "postgres mysql"
//
// Everything talks to the drivers through mantlekeep.Store, so enabling one is a
// build flag plus a config binding — never a core code change.
package store

import (
	"fmt"
	"sort"
	"sync"

	mantlekeep "mantlekeep.dev/control"
)

// Opener builds a Store from a DSN/config string.
type Opener func(dsn string) (mantlekeep.Store, error)

var (
	mu      sync.Mutex
	openers = map[string]Opener{}
)

// Register wires a driver by name. Drivers call this from init(); a driver file
// guarded by a build tag only compiles — and only registers — when that tag is
// set, so an un-tagged build links none of the heavy driver code.
func Register(name string, o Opener) {
	mu.Lock()
	defer mu.Unlock()
	openers[name] = o
}

// Available lists the drivers compiled into THIS binary.
func Available() []string {
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, 0, len(openers))
	for n := range openers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Open builds a store from a registered driver, with a message that names the
// build tag to add if the driver wasn't compiled in.
func Open(name, dsn string) (mantlekeep.Store, error) {
	mu.Lock()
	o, ok := openers[name]
	mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("store driver %q not built in (have %v) — rebuild with `-tags %s`", name, Available(), name)
	}
	return o(dsn)
}
