package dev.mantlekeep.springboot.door;

import java.time.Instant;
import java.util.List;

/**
 * The door's verdict on an {@link Intent}. Immutable, and fails CLOSED: an absent
 * outcome is treated as {@link Outcome#DENY}, so a malformed response never reads as
 * allowed.
 *
 * @param outcome           the verdict (defaults to DENY when {@code null})
 * @param token             the execution token issued on ALLOW (empty otherwise)
 * @param expiresAt         when the token expires ({@code null} when none)
 * @param policyId          the policy that decided
 * @param reasons           denial reasons or warnings (never {@code null})
 * @param requiredApprovers roles that must approve on REQUIRE_APPROVAL (never {@code null})
 */
public record Decision(
        Outcome outcome,
        String token,
        Instant expiresAt,
        String policyId,
        List<String> reasons,
        List<String> requiredApprovers) {

    /** The three verdicts the door can return. */
    public enum Outcome {
        ALLOW,
        DENY,
        REQUIRE_APPROVAL
    }

    public Decision {
        outcome = outcome == null ? Outcome.DENY : outcome; // fail closed
        token = token == null ? "" : token;
        reasons = reasons == null ? List.of() : List.copyOf(reasons);
        requiredApprovers = requiredApprovers == null ? List.of() : List.copyOf(requiredApprovers);
    }

    /** True only when the door allowed the action. */
    public boolean allowed() {
        return outcome == Outcome.ALLOW;
    }
}
