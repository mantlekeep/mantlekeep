module github.com/mantlekeep/mantlekeep/mantlekeep-policy-postgres

// Kept in lockstep with mantlekeep-control/go.mod — the CI toolchain is pinned to the
// patched release that clears the current stdlib advisories, and a module lagging behind
// would be scanned against an older stdlib.
go 1.25.0

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/mantlekeep/mantlekeep/mantlekeep-control v0.1.2
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.21.0 // indirect
	// Held ABOVE what the driver asks for: pgx v5.10.0 is content with x/text v0.29.0, which
	// govulncheck flags for GO-2026-5970 (infinite loop on invalid input), reachable from
	// sql.Open. Raised here rather than waiting for the driver to move, because a module whose
	// gate is red is one nobody can land a change through. Drop this back to the driver's own
	// requirement once pgx requires a fixed version itself.
	golang.org/x/text v0.39.0 // indirect
)

// Build against the core in this repository rather than the published tag, so a change
// to the ports and the adapter that follows it are proven together in one commit. A
// `replace` applies only to the main module, so a downstream consumer of this module
// still resolves the required version from the proxy — this cannot leak a local path
// into someone else's build.
replace github.com/mantlekeep/mantlekeep/mantlekeep-control => ../mantlekeep-control
