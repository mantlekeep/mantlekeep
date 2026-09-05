package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	// The Postgres driver, registered under the name "pgx" for database/sql.
	//
	// It is imported HERE, in an adapter module, and never in the core. The core links only
	// bbolt, and that is load-bearing: a CVE — or a registry quarantine — anywhere in this
	// client's tree must be able to red this module's gate without touching the engine's build.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open wires the Postgres driver and returns a pool.
//
// The CALLER owns what comes back and closes it. That is deliberate rather than tidy: a Store
// that owned its pool would own its credentials' lifetime too, and a deployment that brokers
// credentials — rotating them, scoping them to a lease — needs to hold that itself.
//
// A deployment that already has a pool should skip this entirely and call [New].
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		// Refused, not defaulted to localhost. A policy store that silently connected somewhere
		// other than where the operator meant is the worst possible thing to be wrong about.
		return nil, fmt.Errorf("sqlstore: Open needs a data source name")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: opening the policy database: %w", err)
	}
	// sql.Open does not connect. Reaching the server HERE means a misconfigured policy store
	// fails at startup, rather than on the first governed decision — which is the moment when
	// an unreadable policy is most expensive and least expected.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlstore: cannot reach the policy database: %w", err)
	}
	return db, nil
}
