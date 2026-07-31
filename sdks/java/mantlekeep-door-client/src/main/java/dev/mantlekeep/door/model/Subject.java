package dev.mantlekeep.door.model;

/**
 * WHO is acting — resolved by the host (SSO gateway, Spring Security, dev login),
 * never asserted by the payload. Identity is a property of the call.
 */
public record Subject(String id, String role) {

    public Subject {
        id = id == null || id.isBlank() ? "anonymous" : id;
        role = role == null ? "" : role;
    }

    /** The unauthenticated subject — useful only in dev; the door will say so. */
    public static Subject anonymous() {
        return new Subject("anonymous", "");
    }

    /** A subject known by id alone; the door resolves roles from the directory. */
    public static Subject ofId(String id) {
        return new Subject(id, "");
    }
}
