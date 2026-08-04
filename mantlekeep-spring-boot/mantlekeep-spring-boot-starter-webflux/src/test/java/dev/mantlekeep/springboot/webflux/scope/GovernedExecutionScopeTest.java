package dev.mantlekeep.springboot.webflux.scope;

import static org.junit.jupiter.api.Assertions.assertFalse;

import dev.mantlekeep.springboot.door.ApprovalScope;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.jupiter.api.Test;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

/**
 * The transition-level scope: work runs under one approval token, and if there is no token the
 * work never starts. These pin both halves of the contract — the token is exposed to the work it
 * authorises, and a missing token fails closed before any step runs.
 */
class GovernedExecutionScopeTest {

    private final GovernedExecutionScope scope = new ReactiveGovernedExecutionScope();

    @Test
    void runsUnderTheApprovalToken_andExposesItToTheWork() {
        // The work reads the scope; it must see the token runUnder established.
        Mono<String> result = scope.runUnder("tok-123", ApprovalScope::current);
        StepVerifier.create(result).expectNext("tok-123").verifyComplete();
    }

    @Test
    void failsClosedOnABlankToken_andTheWorkNeverRuns() {
        AtomicBoolean ran = new AtomicBoolean(false);
        Mono<String> result = scope.runUnder("   ", () -> {
            ran.set(true);
            return Mono.just("should not happen");
        });
        StepVerifier.create(result).expectError(NoApprovalScopeException.class).verify();
        assertFalse(ran.get(), "work must not run without an approval scope");
    }

    @Test
    void failsClosedOnANullToken() {
        StepVerifier.create(scope.runUnder(null, () -> Mono.just("x")))
                .expectError(NoApprovalScopeException.class).verify();
    }
}
