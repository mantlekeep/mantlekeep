package dev.mantlekeep.door.model;

import java.util.Map;

/**
 * A governed action, declared BEFORE it runs (declare-before-execute): who asks,
 * what for, why. This is what the door judges and what the chain records.
 *
 * <p>Defaults live in the compact constructor — an intent read back from old stored
 * data with missing fields must still be a valid intent, never a null-field 500.
 */
public record Intent(
        String id,
        Subject subject,
        String action,
        String resource,
        String goal,
        Map<String, ?> parameters) {

    // A WILDCARD, not Map<String,Object>: Java generics are invariant, so a product that
    // naturally declares Map<String,String> would not fit Map<String,Object> and would stop
    // compiling. The wildcard accepts both, and a floor needing nested values can still pass
    // Map<String,Object>. Values are only ever read and serialised, never assigned into, so
    // the weaker type costs nothing here.
    public Intent {
        id = id == null ? "" : id;
        subject = subject == null ? Subject.anonymous() : subject;
        action = action == null ? "" : action;
        resource = resource == null ? "" : resource;
        goal = goal == null ? "" : goal;
        parameters = parameters == null ? Map.of() : Map.copyOf(parameters);
    }

    /** The minimal intent: an action and its WHY. The door requires both. */
    public static Intent of(String action, String goal) {
        return new Intent("", Subject.anonymous(), action, "", goal, Map.of());
    }

    /** This intent re-attributed to the given subject (records are immutable). */
    public Intent asSubject(Subject actingSubject) {
        return new Intent(id, actingSubject, action, resource, goal, parameters);
    }
}
