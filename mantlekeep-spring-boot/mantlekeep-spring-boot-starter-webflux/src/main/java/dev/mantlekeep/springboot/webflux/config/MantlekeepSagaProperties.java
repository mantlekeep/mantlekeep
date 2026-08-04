package dev.mantlekeep.springboot.webflux.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Binds {@code mantlekeep.saga.*} — how much of a governed order's execution the starter persists
 * as a saga timeline.
 *
 * <pre>
 * mantlekeep:
 *   saga:
 *     recording: steps   # none | decisions | steps | full  (default: steps)
 * </pre>
 *
 * <p>{@code recording} is left {@code null} here when absent and resolved by
 * {@link dev.mantlekeep.springboot.saga.RecordingLevel#from(String)}, which defaults to
 * {@code STEPS} on anything unset or unrecognised. This tunes what is <em>recorded</em> only; it
 * never affects whether an order goes through the door — deciding is always on and lands on the
 * audit chain.
 *
 * @param recording {@code mantlekeep.saga.recording} ← {@code MANTLEKEEP_SAGA_RECORDING}
 */
@ConfigurationProperties(prefix = "mantlekeep.saga")
public record MantlekeepSagaProperties(String recording) {
}
