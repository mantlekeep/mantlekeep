package dev.mantlekeep.spring;

import dev.mantlekeep.door.model.Subject;

/**
 * WHO is acting right now — the product's one identity seam. The starter's default
 * resolves a fixed subject from {@code mantlekeep.door.subject}; a real product overrides
 * this with ONE bean backed by its SSO gateway / Spring Security principal. Identity
 * stays a property of the call, never of the payload.
 */
@FunctionalInterface
public interface SubjectResolver {

    /** The subject to attribute the current governed action to. */
    Subject currentSubject();
}
