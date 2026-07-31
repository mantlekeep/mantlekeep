package dev.mantlekeep.springboot.gov;

/**
 * The one rule shared by every governed product: the person who approves an action may not be
 * the person who requested (or proposed) it. Promoted into the SDK so the <em>meaning</em> of
 * "the same actor" — trimming, blank-handling — lives in exactly one place, and no product can
 * quietly adopt a laxer definition.
 *
 * <p>Deliberately a predicate, not a guard that throws. Each product raises its own message with
 * its own vocabulary ("cannot approve their own review role", "requester cannot approve the
 * run"), and its own exception type; forcing a single exception here would leak one product's
 * language into another. The shared contract is the comparison, not the reaction to it.
 *
 * <p>Blank on either side is <strong>not</strong> a violation: an absent actor is a wiring
 * failure the caller must reject explicitly (a gate passed by "" approving "" would be worse
 * than an exception). This helper answers only "are these the same real person?".
 */
public final class SeparationOfDuties {

    private SeparationOfDuties() {
    }

    /**
     * True when {@code approver} and {@code requester} are the same non-blank principal.
     *
     * @param approver  who is trying to approve
     * @param requester who requested or proposed the work
     */
    public static boolean violated(String approver, String requester) {
        if (approver == null || requester == null) {
            return false;
        }
        String a = approver.trim();
        String b = requester.trim();
        return !a.isEmpty() && a.equals(b);
    }
}
