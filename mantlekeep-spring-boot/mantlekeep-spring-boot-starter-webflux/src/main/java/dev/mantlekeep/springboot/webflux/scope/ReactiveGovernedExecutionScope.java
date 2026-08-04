package dev.mantlekeep.springboot.webflux.scope;

import dev.mantlekeep.springboot.door.ApprovalScope;
import java.util.function.Supplier;
import reactor.core.publisher.Mono;

/**
 * The reactive {@link GovernedExecutionScope}: it writes the approval token into the reactive
 * context ({@link ApprovalScope}) so every step running beneath it can prove which decision
 * authorised it, and it refuses — before running anything — when no token is present.
 *
 * <p>It adds no per-step governance: the transition was already decided at the door. It only
 * establishes and carries the scope. The work is deferred so its side effects begin strictly after
 * the scope is in place, never during assembly.
 */
public final class ReactiveGovernedExecutionScope implements GovernedExecutionScope {

    @Override
    public <T> Mono<T> runUnder(String approvalToken, Supplier<Mono<T>> work) {
        if (approvalToken == null || approvalToken.isBlank()) {
            // Fail closed BEFORE step 1: no proof the governing transition was allowed.
            return Mono.error(new NoApprovalScopeException());
        }
        return Mono.defer(work).contextWrite(ApprovalScope.with(approvalToken));
    }
}
