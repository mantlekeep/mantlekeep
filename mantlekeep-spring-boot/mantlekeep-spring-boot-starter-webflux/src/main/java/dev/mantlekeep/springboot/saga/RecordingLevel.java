package dev.mantlekeep.springboot.saga;

import java.util.Locale;

/**
 * How much of a governed order's execution is PERSISTED as a saga timeline — the developer's design
 * choice, scaled to environment. Shared by every product on the starter (mirrors the core
 * orchestrator's levels and {@code docs/recording-levels.md}).
 *
 * <p>The line that does not move: this controls what is <em>recorded</em>, never whether an order
 * goes <em>through the door</em>. Deciding (govern-before-execute) is always on and lands on the
 * audit chain; the saga timeline is this separate, tunable axis. A run can be silent but never
 * ungoverned.
 */
public enum RecordingLevel {

    /** Throwaway local dev. The door still decides and that decision is on the chain; no timeline. */
    NONE,
    /** The chain only (who did what, allow/deny). No per-step timeline. */
    DECISIONS,
    /** Plus the saga timeline: per-step status and the tool's real command/output. */
    STEPS,
    /** Plus the evidence pack (build-domain, EvidencePort) — same timeline as STEPS here. */
    FULL;

    /** Whether this level keeps the per-step saga timeline. Only STEPS and FULL do. */
    public boolean recordsTimeline() {
        return this == STEPS || this == FULL;
    }

    /** Parse a config value, defaulting to STEPS (the useful default) on anything unrecognised. */
    public static RecordingLevel from(String value) {
        if (value == null || value.isBlank()) {
            return STEPS;
        }
        try {
            return RecordingLevel.valueOf(value.trim().toUpperCase(Locale.ROOT));
        } catch (IllegalArgumentException unknown) {
            return STEPS;
        }
    }
}
