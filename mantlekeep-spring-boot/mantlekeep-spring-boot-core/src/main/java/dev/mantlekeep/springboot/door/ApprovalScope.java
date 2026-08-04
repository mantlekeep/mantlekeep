package dev.mantlekeep.springboot.door;

import dev.mantlekeep.door.model.Intent;
import reactor.core.publisher.Mono;
import reactor.util.context.Context;

/**
 * The approval token of a governed TRANSITION, carried through the reactive chain so the steps
 * that run beneath that one approval can prove which decision authorised them — without
 * re-submitting to the door per step.
 *
 * <p>The door decides a transition ONCE (e.g. "approve this run") and issues a token on allow.
 * That token is written here, where the governed method's body runs, and read by
 * {@code GovernedExecutionScope.runUnder} to establish the execution scope. If it is absent — a
 * step reached without passing the governed transition — the scope fails closed and the work never
 * starts. This is the transition-level counterpart to {@link OnBehalfOf}: identity is carried the
 * same way, and so is the authorisation to execute.
 *
 * <p>Deliberately NOT a field on {@link Intent} or a method parameter: an approval token threaded
 * by hand is a token a call site can forge or forget. Carried in the context, it is written once
 * (by the governing aspect, on allow) and read by the scope; nothing in between passes it around.
 */
public final class ApprovalScope {

    private static final String KEY = ApprovalScope.class.getName();

    private ApprovalScope() {
    }

    /** Carry {@code token} as the approval authorising everything downstream. Blank = no scope. */
    public static Context with(String token) {
        return token == null || token.isBlank() ? Context.empty() : Context.of(KEY, token);
    }

    /**
     * The approval token in scope, or empty string when none was established. Absence is not an
     * error here — it is the signal {@code runUnder} turns into a fail-closed refusal, so the
     * decision to refuse lives in one place, not scattered across every caller.
     */
    public static Mono<String> current() {
        return Mono.deferContextual(ctx -> Mono.just(ctx.hasKey(KEY) ? ctx.get(KEY) : ""));
    }
}
