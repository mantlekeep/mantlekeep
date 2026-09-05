package estate

// Ownership says which fields MantleKeep governs and which it merely watches.
//
// Without it a reconciler cries wolf. The platform autoscales replicas, MantleKeep compares replicas
// against the floor, and every scaling event becomes drift — so the report fires constantly on
// legitimate behaviour, people mute it within a week, and the one real unapproved change goes
// unnoticed with everything else. A control people learn to ignore governs nothing.
//
// Two controllers writing one field is the other half of the same problem: they flip it back
// and forth forever, each seeing the other's value as drift. Declaring ownership is how two
// systems share an object without fighting over it.
type Ownership struct {
	// Governed fields are ours: a difference is a VIOLATION and is corrected or escalated.
	Governed map[string]bool
	// Watched fields belong to someone else: a difference is RECORDED and never corrected.
	// Watching rather than ignoring matters — "the platform scaled this to 40" is worth knowing even
	// though it is not ours to change.
	Watched map[string]bool
}

// DefaultOwnership reflects the split as it stands: MantleKeep decides WHAT runs, the runtime
// decides HOW MUCH of it runs at any moment.
func DefaultOwnership() Ownership {
	apps := Ownership{
		Governed: map[string]bool{
			"digest":  true, // WHICH artifact — the whole point of an approval
			"image":   true,
			"runtime": true,
			// The floor's own numbers. These were applied once at provisioning; if nothing
			// checks them again, widening one by hand is invisible and the record still shows
			// what was approved a year ago.
			"retention":           true,
			"producerBytesPerSec": true,
			"consumerBytesPerSec": true,
			"connectionLimit":     true,
			"statementTimeout":    true,
			"robotExpiry":         true,
			"cpuLimit":            true,
			"memoryMiB":           true,
		},
		Watched: map[string]bool{
			// The platform autoscales. Correcting this would fight the autoscaler on a timer.
			"replicas": true,
		},
	}
	// The RUNTIME's fields are declared beside the runtime model. They are merged in here
	// rather than left for a caller to remember, because a fleet field that appears in NEITHER
	// map is skipped by the differ — an omission would silently stop reporting Kubernetes
	// upgrades, and silence reads exactly like "no drift".
	return apps.with(FleetOwnership())
}

// with merges another ownership declaration in. Later declarations add; they never take a
// field away, so no addition can quietly un-govern something already governed.
func (o Ownership) with(other Ownership) Ownership {
	for field := range other.Governed {
		o.Governed[field] = true
	}
	for field := range other.Watched {
		// A field cannot be both. Governed wins: downgrading an owned field to "merely
		// watched" by adding a map entry elsewhere would be a way to remove a control without
		// touching the control.
		if !o.Governed[field] {
			o.Watched[field] = true
		}
	}
	return o
}

// Owns reports whether a difference in this field is ours to act on.
func (o Ownership) Owns(field string) bool { return o.Governed[field] }

// Watches reports whether a difference is worth recording even though it is not ours.
func (o Ownership) Watches(field string) bool { return o.Watched[field] }
