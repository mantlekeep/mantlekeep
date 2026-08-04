package dev.mantlekeep.springboot.saga;

import java.util.List;

/**
 * The saga timeline — the durable trail of what a governed order's executor actually did (env +
 * real command + outcome), separate from the operation ledger (STATE) and the audit chain
 * (DECISION). A port, so a StorePort-backed, chain-correlated adapter drops in later without the
 * product knowing. Shared by every product on the starter.
 *
 * <p>Whether anything is recorded is gated by {@link RecordingLevel} at the call site (see
 * {@link SagaRecorder}); this port is unconditional persistence.
 */
public interface SagaTimeline {

    /** Append one step to the timeline. */
    void record(SagaStep step);

    /** Every step for an operation, in insertion (chronological) order. */
    List<SagaStep> forOperation(String operationId);
}
