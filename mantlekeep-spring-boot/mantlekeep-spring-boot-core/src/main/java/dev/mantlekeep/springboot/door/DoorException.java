package dev.mantlekeep.springboot.door;

/**
 * Signals that the door denied an intent, or that the call to the door failed.
 *
 * <p>When the door returned a verdict, {@link #decision()} carries it (reasons, required
 * approvers). When the failure was transport-level, the decision is {@code null} and the
 * cause is set instead.
 */
public class DoorException extends RuntimeException {

    private final transient Decision decision;

    /** A governance denial: carries the door's decision. */
    public DoorException(String message, Decision decision) {
        super(message);
        this.decision = decision;
    }

    /** A transport-level failure reaching the door. */
    public DoorException(String message, Throwable cause) {
        super(message, cause);
        this.decision = null;
    }

    /** The denial decision, or {@code null} when the failure was transport-level. */
    public Decision decision() {
        return decision;
    }
}
