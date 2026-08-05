package dev.mantlekeep.springboot.saga;

import java.util.List;

/**
 * Records a governed order's saga timeline, gated by the {@link RecordingLevel}. This is the single
 * emission point every product shares, so the "record requested, record executed, respect the level"
 * logic exists ONCE — no product hand-writes it (which is how the two clients drifted before).
 *
 * <p>A product calls {@link #requested} when the door has allowed an order and it begins, and
 * {@link #executed} when the executor returns — passing the real command/result as detail. What is
 * recorded is the product's to describe; WHETHER it is recorded is this class's to gate.
 */
public final class SagaRecorder {

    private final SagaTimeline timeline;
    private final RecordingLevel level;
    private final java.util.function.LongSupplier clock;

    public SagaRecorder(SagaTimeline timeline, RecordingLevel level) {
        this(timeline, level, System::currentTimeMillis);
    }

    // Package-private: a test injects a fixed clock for deterministic timestamps.
    SagaRecorder(SagaTimeline timeline, RecordingLevel level, java.util.function.LongSupplier clock) {
        this.timeline = timeline;
        this.level = level;
        this.clock = clock;
    }

    /** The order was accepted (the door already allowed it) and is beginning. */
    public void requested(String operationId, String subject, String detail) {
        emit(operationId, subject, "requested", "accepted", detail);
    }

    /** The executor returned. {@code detail} should carry the real command and result. */
    public void executed(String operationId, String subject, boolean succeeded, String detail) {
        emit(operationId, subject, "executed", succeeded ? "succeeded" : "failed", detail);
    }

    /**
     * The order was ROUTED to an executor (e.g. an in-zone agent) but has not executed yet — the distinct
     * middle phase between {@link #requested} and {@link #executed} when execution is dispatched, not
     * in-process. {@code detail} should carry WHERE it went (the routing target). The operation stays
     * non-terminal; its {@link #executed} step arrives later when the real outcome returns.
     */
    public void dispatched(String operationId, String subject, String detail) {
        emit(operationId, subject, "dispatched", "routed", detail);
    }

    /** The saga timeline for an operation — a read. */
    public List<SagaStep> forOperation(String operationId) {
        return timeline.forOperation(operationId);
    }

    private void emit(String operationId, String subject, String step, String status, String detail) {
        if (level.recordsTimeline()) {
            timeline.record(new SagaStep(operationId, subject, clock.getAsLong(), step, status, detail));
        }
    }
}
