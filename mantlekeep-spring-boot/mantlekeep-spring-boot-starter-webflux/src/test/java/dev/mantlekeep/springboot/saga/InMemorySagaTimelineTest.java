package dev.mantlekeep.springboot.saga;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import org.junit.jupiter.api.Test;

class InMemorySagaTimelineTest {

    @Test
    void storesAndReturnsStepsPerOperationInInsertionOrder() {
        InMemorySagaTimeline timeline = new InMemorySagaTimeline();

        timeline.record(new SagaStep("op-1", "res-1", 1L, "requested", "accepted", "a"));
        timeline.record(new SagaStep("op-1", "res-1", 2L, "executed", "succeeded", "b"));

        List<SagaStep> steps = timeline.forOperation("op-1");
        assertThat(steps).extracting(SagaStep::step).containsExactly("requested", "executed");
        assertThat(steps).extracting(SagaStep::detail).containsExactly("a", "b");
    }

    @Test
    void keepsOperationsIsolated() {
        InMemorySagaTimeline timeline = new InMemorySagaTimeline();

        timeline.record(new SagaStep("op-1", "res-1", 1L, "requested", "accepted", "a"));
        timeline.record(new SagaStep("op-2", "res-2", 1L, "requested", "accepted", "x"));

        assertThat(timeline.forOperation("op-1")).hasSize(1);
        assertThat(timeline.forOperation("op-2")).hasSize(1);
        assertThat(timeline.forOperation("op-1").get(0).subject()).isEqualTo("res-1");
    }

    @Test
    void returnsEmptyForUnknownOperation() {
        InMemorySagaTimeline timeline = new InMemorySagaTimeline();

        assertThat(timeline.forOperation("missing")).isEmpty();
    }

    @Test
    void returnedListIsAnImmutableSnapshot() {
        InMemorySagaTimeline timeline = new InMemorySagaTimeline();
        timeline.record(new SagaStep("op-1", "res-1", 1L, "requested", "accepted", "a"));

        List<SagaStep> snapshot = timeline.forOperation("op-1");
        timeline.record(new SagaStep("op-1", "res-1", 2L, "executed", "succeeded", "b"));

        // The earlier snapshot does not see the later append.
        assertThat(snapshot).hasSize(1);
        assertThat(timeline.forOperation("op-1")).hasSize(2);
    }
}
