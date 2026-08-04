package dev.mantlekeep.springboot.saga;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class RecordingLevelTest {

    @Test
    void noneAndDecisionsKeepNoTimeline() {
        assertThat(RecordingLevel.NONE.recordsTimeline()).isFalse();
        assertThat(RecordingLevel.DECISIONS.recordsTimeline()).isFalse();
    }

    @Test
    void stepsAndFullKeepTheTimeline() {
        assertThat(RecordingLevel.STEPS.recordsTimeline()).isTrue();
        assertThat(RecordingLevel.FULL.recordsTimeline()).isTrue();
    }

    @Test
    void parsesKnownValuesCaseInsensitively() {
        assertThat(RecordingLevel.from("none")).isEqualTo(RecordingLevel.NONE);
        assertThat(RecordingLevel.from("Decisions")).isEqualTo(RecordingLevel.DECISIONS);
        assertThat(RecordingLevel.from("  FULL  ")).isEqualTo(RecordingLevel.FULL);
    }

    @Test
    void defaultsToStepsOnNullBlankOrJunk() {
        assertThat(RecordingLevel.from(null)).isEqualTo(RecordingLevel.STEPS);
        assertThat(RecordingLevel.from("")).isEqualTo(RecordingLevel.STEPS);
        assertThat(RecordingLevel.from("   ")).isEqualTo(RecordingLevel.STEPS);
        assertThat(RecordingLevel.from("verbose")).isEqualTo(RecordingLevel.STEPS);
    }
}
