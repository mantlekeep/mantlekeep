package dev.mantlekeep.door.model;

/**
 * One link of the hash-chain, as a client sees it. The two door modes surface
 * slightly different fields (the wire view omits internals like the intent id;
 * the embedded core exposes it) — so the parsed common fields sit alongside
 * {@code rawJson}, the untouched record for anything mode-specific.
 */
public record AuditRecord(
        String intentId,
        String subjectId,
        String action,
        String decision,
        String rawJson) {

    public AuditRecord {
        intentId = intentId == null ? "" : intentId;
        subjectId = subjectId == null ? "" : subjectId;
        action = action == null ? "" : action;
        decision = decision == null ? "" : decision;
        rawJson = rawJson == null ? "" : rawJson;
    }
}
