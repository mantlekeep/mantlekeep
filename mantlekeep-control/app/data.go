package app

import (
	"fmt"
	"os"
	"path/filepath"

	"mantlekeep.dev/control/internal/safeio"
)

// DataDir returns MantleKeep's durable data directory — where the audit chain, run
// history, HITL approvals and catalog live as bbolt files. It defaults to
// "./mantlekeep-data" (relative to the working directory, exactly like a sqlite file),
// so a local app is durable out of the box with no configuration: close the laptop,
// reopen, `mantlekeep serve`, and the state is still there. Set MANTLEKEEP_DATA_DIR to place
// it elsewhere (e.g. /var/lib/mantlekeep on a server). The directory is created if
// missing.
//
// This is the persistence tier for the no-Docker local user: no database server, no
// compose, no network — one visible folder of files you can back up (copy) or reset
// (delete). Swap to a shared Postgres later via the Store port; the default stays
// self-contained.
func DataDir() string {
	d := os.Getenv("MANTLEKEEP_DATA_DIR")
	if d == "" {
		d = "mantlekeep-data"
	}
	// Create through the validated config door: 0o750 (owner+group, never world — this
	// holds the audit chain and approvals) and traversal-rejected. Warn and continue on
	// failure, exactly as before, so a read-only environment degrades rather than crashes.
	if _, err := safeio.EnsureConfigDir(d); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create data dir %q: %v\n", d, err)
	}
	return d
}

// dataPath joins a filename onto the durable data directory.
func dataPath(name string) string { return filepath.Join(DataDir(), name) }

// DataPath is the public form of dataPath — so a downstream product module composes the same
// bbolt file paths under the core's data dir without re-deriving the directory logic.
func DataPath(name string) string { return dataPath(name) }
