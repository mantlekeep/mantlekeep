package dev.mantlekeep;

/** The door's verdict on a governed action. */
public record Decision(boolean allowed, String token, String reason) {

    /** True if the door allowed the action. */
    public boolean allowed() {
        return allowed;
    }

    /** The capability token (present on allow), or null. */
    public String token() {
        return token;
    }

    /** Throw {@link MantlekeepDeniedException} if the action was denied — a one-line guard. */
    public void require() {
        if (!allowed) {
            throw new MantlekeepDeniedException(reason == null ? "denied by policy" : reason);
        }
    }
}
