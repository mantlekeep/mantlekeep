// Package pgpolicy holds governance policy in Postgres instead of in a file.
//
// It implements the two ports the core defines in
// github.com/mantlekeep/mantlekeep/mantlekeep-control/grants: [grants.Loader], so every
// replica reads the policy in force from one place, and [grants.Writer], so
// [grants.Govern] can apply a change the door has already allowed.
//
// It is an ADAPTER. It decides nothing. By the time Write is called the door has allowed
// the change, and this package's only job is to make the rows say what was approved — and
// to report what the rows actually hold, never what a request asked for.
//
// # Why a database rather than a file
//
// A file source is read by every replica and written by none: policy changes arrive as a
// deploy. That is the right answer for a deployment whose policy is a release artefact, and
// it is the default. It is the wrong answer for a deployment that edits policy through a UI
// — there, a change must take effect without a rollout, every replica must see the same
// change at the same moment, and somebody must be able to ask what the policy said last
// Tuesday. That is what this module is for. See README.md for the choice and the migration
// path in both directions.
//
// # What this package refuses to do
//
//   - It never answers a failed read with an empty policy. Empty grants deny everything, so
//     a source that failed and one that legitimately grants nothing would be
//     indistinguishable — and the first would look like a working deny-all rather than an
//     outage. Every failure is an error.
//   - It never derives a revision from a row id, a sequence number or a timestamp. The
//     revision comes from [grants.RevisionOf] over the documents as loaded, so a file
//     deployment and a database deployment serving identical policy agree — which is what
//     lets one be migrated to the other and PROVED equivalent rather than assumed to be.
//   - It never second-guesses an approved change. A writer that refused a change the door
//     allowed would be a policy engine underneath the policy engine, in a place no floor
//     can see and no chain records.
//   - It never rewrites the parts of the document a change did not name. The writer touches
//     exactly the one entry named and leaves every other byte alone, so a grant followed by
//     its revoke returns the store to the revision it started from.
//
// # Dependency direction
//
// This module depends on the core as a library, one way, never back. It is a SEPARATE Go
// module precisely so the Postgres driver never becomes a dependency of the core: a CVE — or
// a registry quarantine — in the pgx tree must not be able to block the engine's build. The
// core links only bbolt, and nothing here may change that.
//
// # Testing
//
// Postgres is reached through the [Store] interface, so every rule in this package is decided
// without a database and every one of them is unit-tested with a fake. The SQL that makes
// those rules true — the row lock, the transaction, the compare-and-set, the append-only
// history — lives in the sqlstore subpackage and is proved against a real server, because a
// fake cannot demonstrate that Postgres serialises two writers and assuming it is exactly the
// mistake this module must not make. Set MANTLEKEEP_POSTGRES_TEST_DSN to run those; without
// it they skip, and the rest of the suite still runs everywhere.
package pgpolicy
