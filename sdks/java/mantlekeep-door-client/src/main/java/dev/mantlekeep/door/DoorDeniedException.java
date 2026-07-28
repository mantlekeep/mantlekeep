package dev.mantlekeep.door;

import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;

/**
 * The door said no. Thrown by {@link DoorClient#submit} BEFORE any effect runs —
 * a deny aborts, it never degrades. Carries the denied intent's action and the
 * door's reason so the failure reads like the audit record it mirrors.
 */
public final class DoorDeniedException extends RuntimeException {

    private final String action;
    private final String reason;

    public DoorDeniedException(Intent deniedIntent, Decision denial) {
        super("the door denied '" + deniedIntent.action() + "' for subject '"
                + deniedIntent.subject().id() + "'"
                + (denial.reason().isBlank() ? "" : ": " + denial.reason()));
        this.action = deniedIntent.action();
        this.reason = denial.reason();
    }

    /** The governed action that was denied. */
    public String action() {
        return action;
    }

    /** The door's stated reason — the same text that landed on the chain. */
    public String reason() {
        return reason;
    }
}
