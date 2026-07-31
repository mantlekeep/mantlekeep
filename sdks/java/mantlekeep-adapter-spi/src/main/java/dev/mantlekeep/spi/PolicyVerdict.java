package dev.mantlekeep.spi;

/**
 * A {@link PolicyEvaluator}'s answer: allow or deny, with the reason on deny.
 * The reason is part of the contract — a bare "no" is not governable evidence.
 */
public record PolicyVerdict(boolean allowed, String reason) {

    public PolicyVerdict {
        reason = reason == null ? "" : reason;
    }

    /** An allow, no reason needed. */
    public static PolicyVerdict allow() {
        return new PolicyVerdict(true, "");
    }

    /** A deny with its reason — the reason lands on the chain and in the thrown error. */
    public static PolicyVerdict deny(String reason) {
        return new PolicyVerdict(false, reason);
    }
}
