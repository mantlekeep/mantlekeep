package dev.mantlekeep.springboot.webflux.identity;

import reactor.core.publisher.Mono;
import reactor.util.context.Context;

/**
 * Carries the resolved {@link Caller} through the reactive chain.
 *
 * <p>A request-scoped bean is not available in WebFlux — a request is handled across threads —
 * so the caller travels in the Reactor context, written once by the filter and read wherever
 * it is needed. Reading is deliberately fail-fast: reaching a governed operation with no
 * caller is a wiring bug, and returning a placeholder would recreate the hole this closes.
 */
public final class CallerContext {

    private static final Class<Caller> KEY = Caller.class;

    private CallerContext() {
    }

    /** Write the caller into the context for everything downstream. */
    public static Context with(Caller caller) {
        return Context.of(KEY, caller);
    }

    /** The caller for the current request; errors if the filter did not establish one. */
    public static Mono<Caller> current() {
        return Mono.deferContextual(ctx -> ctx.hasKey(KEY)
                ? Mono.just(ctx.get(KEY))
                : Mono.error(new IllegalStateException(
                        "no authenticated caller in context — CallerWebFilter did not run")));
    }
}
