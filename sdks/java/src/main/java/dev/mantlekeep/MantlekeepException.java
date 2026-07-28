package dev.mantlekeep;

/** A failure talking to the MantleKeep core (network, malformed response…). */
public class MantlekeepException extends RuntimeException {
    public MantlekeepException(String message) { super(message); }
    public MantlekeepException(String message, Throwable cause) { super(message, cause); }
}
