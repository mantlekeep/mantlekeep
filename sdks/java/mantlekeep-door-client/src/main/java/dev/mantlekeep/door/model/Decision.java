package dev.mantlekeep.door.model;

import java.util.List;

/**
 * The door's verdict — the enterprise contract, not a yes/no. Three outcomes
 * ({@link Outcome#ALLOW}, {@link Outcome#DENY}, {@link Outcome#REQUIRE_APPROVAL}), the
 * policy that decided, typed reasons an auditor can query without parsing prose, the
 * approvers a {@code REQUIRE_APPROVAL} still needs, and when an allow's token expires.
 * Either way the decision is already ON THE CHAIN by the time a caller holds this.
 *
 * <p>This is the shape the Go core already decides internally; the door wire now carries
 * it whole, so a Java product reads who-must-approve / under-which-policy / why / valid-until
 * instead of inferring them.
 */
public record Decision(
        Outcome outcome,
        String token,
        String policyId,
        List<Reason> reasons,
        List<String> requiredApprovers,
        String expiresAt) {

    /** The three verdicts the door can return. */
    public enum Outcome {
        ALLOW,
        DENY,
        REQUIRE_APPROVAL;

        /** Maps a wire outcome string to the enum; anything unrecognised is a DENY (fail closed). */
        public static Outcome fromWire(String wire) {
            if (wire == null) {
                return DENY;
            }
            return switch (wire) {
                case "allow" -> ALLOW;
                case "require_approval" -> REQUIRE_APPROVAL;
                default -> DENY;
            };
        }
    }

    /** One typed reason: a stable code the product switches on, plus the human message. */
    public record Reason(String code, String message) {
        public Reason {
            code = code == null ? "" : code;
            message = message == null ? "" : message;
        }
    }

    public Decision {
        // A field read back null from old data is a 500 waiting to happen — default here so a
        // caller never has to null-check the verdict.
        outcome = outcome == null ? Outcome.DENY : outcome;
        token = token == null ? "" : token;
        policyId = policyId == null ? "" : policyId;
        reasons = reasons == null ? List.of() : List.copyOf(reasons);
        requiredApprovers = requiredApprovers == null ? List.of() : List.copyOf(requiredApprovers);
        expiresAt = expiresAt == null ? "" : expiresAt;
    }

    /** An allow carrying only its token — for test doubles and the embedded path. */
    public static Decision allow(String token) {
        return new Decision(Outcome.ALLOW, token, "", List.of(), List.of(), "");
    }

    /** A deny carrying a single untyped reason — for test doubles and the embedded path. */
    public static Decision deny(String reason) {
        return new Decision(Outcome.DENY, "", "",
                List.of(new Reason("", reason == null ? "" : reason)), List.of(), "");
    }

    /** Compatibility accessor: an allow, and only an allow, is "allowed". */
    public boolean allowed() {
        return outcome == Outcome.ALLOW;
    }

    /**
     * Compatibility accessor: the first reason's human message, or empty. Callers written
     * against the old flat {@code reason} field keep reading, while new callers walk
     * {@link #reasons()} for the typed codes.
     */
    public String reason() {
        return reasons.isEmpty() ? "" : reasons.get(0).message();
    }
}
