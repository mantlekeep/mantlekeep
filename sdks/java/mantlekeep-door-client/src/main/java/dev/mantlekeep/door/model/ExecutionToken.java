package dev.mantlekeep.door.model;

/**
 * The capability the door issues on allow — proof the effect that follows was
 * governed. Workers demand it; an effect without a token is an effect the door
 * never approved.
 */
public record ExecutionToken(String value) {

    public ExecutionToken {
        value = value == null ? "" : value;
    }
}
