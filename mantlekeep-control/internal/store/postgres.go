//go:build postgres

// This file is compiled ONLY when you build with `-tags postgres`. That is what
// keeps the heavy driver optional: in a normal build it does not exist, so its
// dependency is never linked and can never affect the core. Enable it with:
//
//	go build -tags postgres ./cmd/mantle
//
// In a real deployment the body opens a live pool, e.g.:
//
//	import "github.com/jackc/pgx/v5/pgxpool"
//	pool, err := pgxpool.New(context.Background(), dsn)
//	return &pgStore{pool: pool}, err
//
// It is stubbed here (stdlib only) so the OPT-IN MECHANISM is demonstrable
// without pulling pgx into this repo — replace the body with the real driver and
// the build tag keeps it out of every default build.

package store

import mantlekeep "mantlekeep.dev/control"

func init() { Register("postgres", openPostgres) }

// openPostgres would return a Postgres-backed mantlekeep.Store; stubbed to an
// in-memory store so the tag/registration path is exercised without the dep.
func openPostgres(dsn string) (mantlekeep.Store, error) {
	return NewMem(), nil
}
