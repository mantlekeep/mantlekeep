package dev.mantlekeep.spring;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Marks a method as a GOVERNED action: before the body runs, the intent is submitted
 * through the one door; a deny throws and the body never executes — the same
 * interception shape as Spring Security's {@code @PreAuthorize}, but judged by
 * MantleKeep's governed door and recorded on the hash-chain.
 */
@Target(ElementType.METHOD)
@Retention(RetentionPolicy.RUNTIME)
public @interface MantlekeepIntent {

    /** The governed action name, e.g. {@code "job.promote"}. Required. */
    String action();

    /** The resource the action touches, e.g. {@code "project/demo"}. */
    String resource() default "";

    /** WHY — declare-before-execute. Defaults to the intercepted method's signature. */
    String goal() default "";
}
