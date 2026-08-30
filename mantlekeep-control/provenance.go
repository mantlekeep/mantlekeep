package mantlekeep

import (
	"fmt"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Provenance — HOW a fact was learned. Verified, or been told.
// ─────────────────────────────────────────────────────────────────────────────

// Provenance distinguishes something the platform verified from something it was told.
//
// A governance system frequently cannot observe what it governs. The zone is air-gapped and
// nothing crosses live; the work ran on a worker that reports its own result; the clusters
// belong to another control plane and no credentials exist for them. In every case a fact
// still has to be recorded, and the honest question is not whether to record it but whether
// the record admits how it was obtained.
//
// Most systems flatten this. They store one "current state" and the reader assumes the
// platform saw it — so testimony is read as proof, and nothing in the record says otherwise.
// Making provenance structural is what keeps a chain honest in exactly the situations where
// honesty is hardest to check.
type Provenance string

const (
	// Firsthand means the platform read it itself. Evidence.
	Firsthand Provenance = "firsthand"
	// Reported means a named principal told the platform. Testimony — worth recording, and
	// worth marking, because a principal can be wrong or lying and the record must survive
	// finding out which.
	Reported Provenance = "reported"
)

// Observation is one fact, and how it came to be known.
//
// Source is REQUIRED when Reported and empty when Firsthand. An anonymous report is a rumour;
// recording one as fact is how a chain launders a guess into evidence. Naming the principal
// makes a wrong report attributable rather than a mystery.
type Observation struct {
	Provenance Provenance `json:"provenance"`
	// Source names the principal that reported this. Empty for a firsthand observation, which
	// needs no attribution beyond the platform itself.
	Source string    `json:"source,omitempty"`
	At     time.Time `json:"at"`
}

// Observed returns a firsthand observation: the platform read this itself.
func Observed(at time.Time) Observation {
	return Observation{Provenance: Firsthand, At: at.UTC()}
}

// ReportedBy returns testimony from a named principal.
//
// It refuses an empty source rather than accepting an anonymous one. A report nobody can be
// held to is not weaker evidence — it is a different thing entirely, and admitting it would
// make every other Reported record less trustworthy by association.
func ReportedBy(source string, at time.Time) (Observation, error) {
	if source == "" {
		return Observation{}, fmt.Errorf(
			"provenance: a reported observation must name the principal that reported it — " +
				"an anonymous report cannot be attributed, corrected, or disbelieved")
	}
	return Observation{Provenance: Reported, Source: source, At: at.UTC()}, nil
}

// Verified reports whether the platform saw this itself.
func (o Observation) Verified() bool { return o.Provenance == Firsthand }

// Prefer returns the observation to believe when two describe the same fact.
//
// Firsthand always wins. The two are NEVER averaged and the more convenient is never chosen:
// a platform that prefers testimony when it is easier has stopped being a record of what
// happened and become a record of what it was told.
//
// This decides which to BELIEVE. It does not decide which to keep — see [Disagree].
func Prefer(a, b Observation) Observation {
	if a.Verified() && !b.Verified() {
		return a
	}
	if b.Verified() && !a.Verified() {
		return b
	}
	// Both the same kind: the later one is the current one.
	if b.At.After(a.At) {
		return b
	}
	return a
}

// Disagree reports whether firsthand evidence and testimony describe the same fact differently.
//
// The disagreement is the finding, and it must never be resolved silently. When a principal's
// report does not match what the platform read, at least one of them is wrong — and which one
// is a question worth a human, not a tiebreak worth a function.
func Disagree(firsthand, reported Observation, sameFact bool) bool {
	return firsthand.Verified() && !reported.Verified() && !sameFact
}
