package dev.mantlekeep.door;

import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.ExecutionToken;
import dev.mantlekeep.door.model.Intent;
import java.util.List;

/**
 * The one door, as a Java product sees it. ONE client, TWO modes by config
 * (composition-model §4b — "the DoorClient is a data-source; the door is the
 * database"): {@link ServiceDoorClient} calls the shared remote door;
 * {@link EmbeddedDoorClient} carries a real in-process core. Products depend on
 * THIS interface only; {@link DoorClientFactory} picks the implementation.
 */
public interface DoorClient extends AutoCloseable {

    /**
     * Asks the door to judge one intent, without throwing — for callers that want
     * to branch on the verdict. The decision is on the chain either way.
     */
    Decision decide(Intent intent);

    /**
     * Govern-before-execute in one call: submits the intent and returns the
     * execution token on allow. On deny it throws — the effect after this line
     * simply never runs.
     *
     * @throws DoorDeniedException when the door denies, carrying the reason
     */
    default ExecutionToken submit(Intent intent) {
        Decision decision = decide(intent);
        if (!decision.allowed()) {
            throw new DoorDeniedException(intent, decision);
        }
        return new ExecutionToken(decision.token());
    }

    /** The chain, as this door sees it — every decision, allow and deny alike. */
    List<AuditRecord> audit();

    /** Walks the hash-chain; {@code true} = intact, untampered. */
    boolean verify();

    /** Releases the client's resources (native core handle / HTTP client). */
    @Override
    void close();
}
