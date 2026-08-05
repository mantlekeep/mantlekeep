package dev.mantlekeep.springboot.saga;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import org.junit.jupiter.api.Test;

class SagaRecorderTest {

    private static final long FIXED_MILLIS = 1_700_000_000_000L;

    private SagaRecorder recorderAt(RecordingLevel level, SagaTimeline timeline) {
        // Package-private fixed-clock constructor — deterministic timestamps.
        return new SagaRecorder(timeline, level, () -> FIXED_MILLIS);
    }

    @Test
    void noneRecordsNothing() {
        SagaTimeline timeline = new InMemorySagaTimeline();
        SagaRecorder recorder = recorderAt(RecordingLevel.NONE, timeline);

        recorder.requested("op-1", "res-1", "accepted");
        recorder.executed("op-1", "res-1", true, "ran ok");

        assertThat(recorder.forOperation("op-1")).isEmpty();
        assertThat(timeline.forOperation("op-1")).isEmpty();
    }

    @Test
    void decisionsRecordsNothing() {
        SagaTimeline timeline = new InMemorySagaTimeline();
        SagaRecorder recorder = recorderAt(RecordingLevel.DECISIONS, timeline);

        recorder.requested("op-1", "res-1", "accepted");
        recorder.executed("op-1", "res-1", true, "ran ok");

        assertThat(recorder.forOperation("op-1")).isEmpty();
    }

    @Test
    void dispatchedRecordsTheRoutingPhase() {
        SagaTimeline timeline = new InMemorySagaTimeline();
        SagaRecorder recorder = recorderAt(RecordingLevel.STEPS, timeline);

        recorder.requested("op-d", "res-d", "begin");
        recorder.dispatched("op-d", "res-d", "cluster-a via agent");   // routed, not yet executed
        recorder.executed("op-d", "res-d", true, "ran in-zone");

        List<SagaStep> steps = recorder.forOperation("op-d");
        assertThat(steps).hasSize(3);
        SagaStep dispatched = steps.get(1);
        assertThat(dispatched.step()).isEqualTo("dispatched");
        assertThat(dispatched.status()).isEqualTo("routed");
        assertThat(dispatched.detail()).isEqualTo("cluster-a via agent");
    }

    @Test
    void stepsRecordsRequestedThenExecutedInOrder() {
        SagaTimeline timeline = new InMemorySagaTimeline();
        SagaRecorder recorder = recorderAt(RecordingLevel.STEPS, timeline);

        recorder.requested("op-1", "res-1", "begin");
        recorder.executed("op-1", "res-1", true, "docker run …");

        List<SagaStep> steps = recorder.forOperation("op-1");
        assertThat(steps).hasSize(2);

        SagaStep requested = steps.get(0);
        assertThat(requested.operationId()).isEqualTo("op-1");
        assertThat(requested.subject()).isEqualTo("res-1");
        assertThat(requested.at()).isEqualTo(FIXED_MILLIS);
        assertThat(requested.step()).isEqualTo("requested");
        assertThat(requested.status()).isEqualTo("accepted");
        assertThat(requested.detail()).isEqualTo("begin");

        SagaStep executed = steps.get(1);
        assertThat(executed.step()).isEqualTo("executed");
        assertThat(executed.status()).isEqualTo("succeeded");
        assertThat(executed.at()).isEqualTo(FIXED_MILLIS);
        assertThat(executed.detail()).isEqualTo("docker run …");
    }

    @Test
    void fullAlsoRecords() {
        SagaTimeline timeline = new InMemorySagaTimeline();
        SagaRecorder recorder = recorderAt(RecordingLevel.FULL, timeline);

        recorder.requested("op-1", "res-1", "begin");

        assertThat(recorder.forOperation("op-1")).hasSize(1);
    }

    @Test
    void executedMarksFailureWhenNotSucceeded() {
        SagaTimeline timeline = new InMemorySagaTimeline();
        SagaRecorder recorder = recorderAt(RecordingLevel.STEPS, timeline);

        recorder.executed("op-1", "res-1", false, "exit 1");

        SagaStep step = recorder.forOperation("op-1").get(0);
        assertThat(step.step()).isEqualTo("executed");
        assertThat(step.status()).isEqualTo("failed");
    }
}
