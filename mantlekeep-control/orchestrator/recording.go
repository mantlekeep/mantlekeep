package orchestrator

// RecordingLevel is how much of a run's timeline is PERSISTED. It is the developer's
// design choice, scaled to environment (a dev sandbox is light; production is full) —
// see docs/recording-levels.md.
//
// The line that does not move: the recording level controls what is *persisted*, never
// whether an action goes *through the door*. Deciding (govern-before-execute) is always
// on and lives on the audit chain; recording is this separate, tunable axis. A run can be
// silent but never ungoverned.
type RecordingLevel string

const (
	// RecordNone — a throwaway dev loop. The door still decides each governed action and
	// that decision is on the chain; no per-step timeline is kept.
	RecordNone RecordingLevel = "none"
	// RecordDecisions — the chain only (who did what, allow/deny). No per-step timeline.
	RecordDecisions RecordingLevel = "decisions"
	// RecordSteps — plus the saga timeline: per-step status, compensation order, the
	// step's real output. This is the default: it is what the Engine has always emitted.
	RecordSteps RecordingLevel = "steps"
	// RecordFull — plus the evidence pack (source snapshot, SBOM, artifacts). Production /
	// regulated. The pack itself is the build-domain EvidencePort, out of the core engine's
	// scope; at this level the Engine records the same timeline as RecordSteps.
	RecordFull RecordingLevel = "full"
)

// recordsTimeline reports whether this level keeps the per-step saga timeline. Only
// `steps` and `full` do; `none` and `decisions` keep the chain alone. An unset/unknown
// level is treated as `steps`, so the default and any older caller keep emitting.
func (l RecordingLevel) recordsTimeline() bool {
	switch l {
	case RecordNone, RecordDecisions:
		return false
	default: // RecordSteps, RecordFull, "" (default), or any unknown value
		return true
	}
}
