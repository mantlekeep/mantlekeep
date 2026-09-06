// Package sqlstore holds MantleKeep policy in a real Postgres database.
//
// It implements pgpolicy.Store — the seam onto the rows — and nothing else. Every RULE about
// policy (what a revision is, how a change is applied, what happens when two operators write
// at once) lives in the parent package and is decided without a database. What lives HERE is
// only the SQL that makes those rules true: the transaction, the compare-and-set, the
// append-only insert.
//
// That split is why the parent package needs no server to be tested and why this one does. A
// fake cannot prove that Postgres serialises two writers correctly — only Postgres can — so
// this package carries an integration test that runs against a real server when
// MANTLEKEEP_POSTGRES_TEST_DSN names one, and skips when it does not. A test that faked the
// database at this layer would be testing the fake's idea of concurrency, which is exactly
// the thing that must not be assumed.
//
// # Connections
//
// New takes an existing *sql.DB and the caller keeps ownership of it: its pool sizing, its
// TLS, and above all its credentials, which a deployment brokers rather than lets this
// package discover. Open is the convenience for a deployment that has no pool yet — it wires
// the pgx driver and hands back a *sql.DB the caller still closes.
//
// The driver lives in THIS module. The core links only bbolt, and a CVE or a registry
// quarantine in the Postgres client tree must not be able to block the engine's build.
package sqlstore
