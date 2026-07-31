package dev.mantlekeep.door;

import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import java.util.List;
import java.util.concurrent.atomic.AtomicLong;

/**
 * The embedded mode: a REAL governance core in-process behind the {@link NativeCore}
 * port — real allow/deny, real hash-chain, zero infra (the H2/SQLite of doors,
 * composition-model §4b). Tests and dev run against it daily; the sovereign air-gap
 * zone runs it in production, carrying its own local door + chain (§4f).
 *
 * <p>Which binding satisfies {@link NativeCore} is the factory's ServiceLoader
 * concern; this class only speaks the core's small JSON contract.
 */
public final class EmbeddedDoorClient implements DoorClient {

    private final NativeCore core;
    private final AtomicLong intentSequence = new AtomicLong();

    public EmbeddedDoorClient(NativeCore core) {
        this.core = core;
    }

    @Override
    public Decision decide(Intent intent) {
        String intentId = intent.id().isBlank()
                ? "JINT-%03d".formatted(intentSequence.incrementAndGet())
                : intent.id();
        String intentJson = "{"
                + "\"id\":" + JsonText.quote(intentId) + ","
                + "\"subject_id\":" + JsonText.quote(intent.subject().id()) + ","
                + "\"action\":" + JsonText.quote(intent.action()) + ","
                + "\"resource\":" + JsonText.quote(intent.resource()) + ","
                + "\"goal\":" + JsonText.quote(intent.goal()) + ","
                + "\"params\":" + JsonText.object(intent.parameters())
                + "}";
        String resultJson = core.submitJson(intentJson);
        boolean allowed = "allow".equals(JsonText.stringField(resultJson, "action"));
        return allowed
                ? Decision.allow(JsonText.stringField(resultJson, "token"))
                : Decision.deny(JsonText.stringField(resultJson, "reason"));
    }

    @Override
    public List<AuditRecord> audit() {
        return JsonText.objectsOfArray(core.auditJson()).stream()
                .map(recordJson -> new AuditRecord(
                        JsonText.stringField(recordJson, "intent_id"),
                        JsonText.stringField(recordJson, "subject_id"),
                        JsonText.stringField(recordJson, "action"),
                        JsonText.stringField(recordJson, "decision"),
                        recordJson))
                .toList();
    }

    @Override
    public boolean verify() {
        return core.verifyChain();
    }

    @Override
    public void close() {
        core.close();
    }
}
