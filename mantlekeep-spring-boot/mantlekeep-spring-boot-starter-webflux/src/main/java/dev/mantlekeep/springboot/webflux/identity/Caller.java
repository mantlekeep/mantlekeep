package dev.mantlekeep.springboot.webflux.identity;

import java.util.Set;

/**
 * The authenticated caller behind a request — resolved from the gateway, never read from a
 * request body or query parameter.
 *
 * <p>This is the type that makes separation of duties real. While the actor was a
 * client-supplied string, "the approver is not the proposer" compared two values the caller
 * controlled, so any gate could be passed as anyone by editing a URL. A {@code Caller} is
 * established before the request reaches a controller and cannot be chosen by the request.
 *
 * @param name  the principal's stable name (e.g. {@code arch-carol}); never blank
 * @param roles the roles the gateway asserts for this principal; never {@code null}
 */
public record Caller(String name, Set<String> roles) {

    public Caller {
        if (name == null || name.isBlank()) {
            throw new IllegalArgumentException("caller name is required");
        }
        name = name.trim();
        roles = roles == null ? Set.of() : Set.copyOf(roles);
    }

    /** A caller with no asserted roles — authorisation still rests with the door. */
    public static Caller named(String name) {
        return new Caller(name, Set.of());
    }

    /** True if the gateway asserted this role. Convenience only — the door decides. */
    public boolean hasRole(String role) {
        return roles.contains(role);
    }
}
