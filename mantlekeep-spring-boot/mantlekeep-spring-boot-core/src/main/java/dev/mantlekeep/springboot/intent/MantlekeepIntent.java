package dev.mantlekeep.springboot.intent;

import java.lang.annotation.Documented;
import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Marks a method as a governed intent.
 *
 * <p>Before the annotated method's effect runs, the starter's aspect builds an
 * {@link dev.mantlekeep.door.model.Intent} and submits it through the
 * {@link dev.mantlekeep.springboot.door.DoorClient}; the method proceeds only on ALLOW.
 * Governance becomes declarative — no hand-written door call per method.
 *
 * <pre>{@code
 * @MantlekeepIntent(value = "loop.propose", resource = "loop/#{#id}")
 * public Mono<Loop> propose(String id, String output) { ... }
 * }</pre>
 */
@Documented
@Target(ElementType.METHOD)
@Retention(RetentionPolicy.RUNTIME)
public @interface MantlekeepIntent {

    /** The governed action, e.g. {@code "loop.propose"}. Required. */
    String value();

    /** The scope target, optionally a SpEL expression; default empty. */
    String resource() default "";

    /** A human goal for the intent; default empty (the aspect may derive one). */
    String goal() default "";
}
