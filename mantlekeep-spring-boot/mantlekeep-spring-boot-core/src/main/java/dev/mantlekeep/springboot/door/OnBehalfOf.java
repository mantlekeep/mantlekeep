package dev.mantlekeep.springboot.door;

import reactor.core.publisher.Mono;
import reactor.util.context.Context;

/**
 * The subject an application acts for, carried through the reactive chain.
 *
 * <p>The door authenticates an APPLICATION. Left there, every record on the chain names the
 * same service — true, and useless, because every action in a product arrives through it. So
 * an application that has authenticated may additionally assert who it acts for, and the door
 * decides whether it is permitted to.
 *
 * <p>This is deliberately NOT a field on {@link Intent}. An intent describes what is asked;
 * identity stays a property of the call. Putting an actor in the payload would make identity
 * something any caller can claim, which is the hole that authentication exists to close.
 *
 * <p>A product writes the value once, where it resolves its caller; adapters read it. Nothing
 * in between passes it around, so no call site can forget to.
 */
public final class OnBehalfOf {

    private static final String KEY = OnBehalfOf.class.getName();

    private OnBehalfOf() {
    }

    /** Carry {@code subject} as the actor for everything downstream. */
    public static Context with(String subject) {
        return subject == null || subject.isBlank() ? Context.empty() : Context.of(KEY, subject);
    }

    /**
     * The asserted subject, or empty when the application acts as itself. Absence is normal
     * and must never be an error: an application with no caller context is acting for nobody,
     * and the door records it as itself.
     */
    public static Mono<String> current() {
        return Mono.deferContextual(ctx -> Mono.just(ctx.hasKey(KEY) ? ctx.get(KEY) : ""));
    }
}
