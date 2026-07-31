package dev.mantlekeep;

/** Thrown by {@link Decision#require()} when the door denies an action. */
public class MantlekeepDeniedException extends MantlekeepException {
    public MantlekeepDeniedException(String reason) { super("MantleKeep denied: " + reason); }
}
