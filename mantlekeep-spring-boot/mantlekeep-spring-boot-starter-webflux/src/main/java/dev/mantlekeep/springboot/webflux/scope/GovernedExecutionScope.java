package dev.mantlekeep.springboot.webflux.scope;

import java.util.function.Supplier;
import reactor.core.publisher.Mono;

/**
 * Runs work beneath a single, transition-level approval — the WebFlux-native answer to
 * "govern once, then execute all the steps".
 *
 * <p>The governed transition (e.g. approving a run) is decided ONCE at the door; on allow it
 * yields an approval token. The saga's steps then run under {@link #runUnder}, which carries that
 * token as the execution scope. There is NO per-step re-submission to the door — re-asking per
 * step would turn transition-level governance into phase-level, which is exactly the shape a
 * governed pipeline must avoid.
 *
 * <p>Fails closed: if no approval scope can be established (a blank token — a step reached without
 * passing the governed transition), the work never starts. On-behalf-of identity already flows
 * through the reactive context, so the steps stay attributable to the human, not the service.
 */
public interface GovernedExecutionScope {

    /**
     * Execute {@code work} under {@code approvalToken}. On a blank token the returned Mono errors
     * (fails closed) before {@code work} is ever invoked.
     *
     * @param approvalToken the token issued when the transition was allowed
     * @param work          the steps to run beneath that one approval
     */
    <T> Mono<T> runUnder(String approvalToken, Supplier<Mono<T>> work);
}
