package dev.mantlekeep.door.model;

/**
 * The door's verdict: allow (with the execution token) or deny (with the reason).
 * Either way the decision is already ON THE CHAIN by the time a caller holds this.
 */
public record Decision(boolean allowed, String reason, String token) {

    public Decision {
        reason = reason == null ? "" : reason;
        token = token == null ? "" : token;
    }

    public static Decision allow(String token) {
        return new Decision(true, "", token);
    }

    public static Decision deny(String reason) {
        return new Decision(false, reason, "");
    }
}
