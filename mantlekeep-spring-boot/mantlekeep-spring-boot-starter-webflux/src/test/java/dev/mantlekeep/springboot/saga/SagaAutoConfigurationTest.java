package dev.mantlekeep.springboot.saga;

import static org.assertj.core.api.Assertions.assertThat;

import dev.mantlekeep.springboot.webflux.config.MantlekeepAutoConfiguration;
import org.junit.jupiter.api.Test;
import org.springframework.boot.autoconfigure.AutoConfigurations;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;

class SagaAutoConfigurationTest {

    private final ApplicationContextRunner runner = new ApplicationContextRunner()
            .withConfiguration(AutoConfigurations.of(MantlekeepAutoConfiguration.class));

    @Test
    void autoConfiguresTimelineAndRecorderWithDefaultRecording() {
        runner.run(ctx -> {
            assertThat(ctx).hasSingleBean(SagaTimeline.class);
            assertThat(ctx.getBean(SagaTimeline.class)).isInstanceOf(InMemorySagaTimeline.class);

            // Default recording is STEPS, so the recorder writes to the timeline.
            SagaRecorder recorder = ctx.getBean(SagaRecorder.class);
            recorder.requested("op-1", "res-1", "begin");
            assertThat(recorder.forOperation("op-1")).hasSize(1);
        });
    }

    @Test
    void recordingPropertyGatesTheRecorder() {
        runner.withPropertyValues("mantlekeep.saga.recording=none").run(ctx -> {
            SagaRecorder recorder = ctx.getBean(SagaRecorder.class);
            recorder.requested("op-1", "res-1", "begin");
            recorder.executed("op-1", "res-1", true, "ran");
            assertThat(recorder.forOperation("op-1")).isEmpty();
        });
    }

    @Test
    void backsOffWhenApplicationDefinesOwnTimeline() {
        InMemorySagaTimeline custom = new InMemorySagaTimeline();
        runner.withBean("sagaTimeline", SagaTimeline.class, () -> custom)
                .run(ctx -> assertThat(ctx.getBean(SagaTimeline.class)).isSameAs(custom));
    }
}
