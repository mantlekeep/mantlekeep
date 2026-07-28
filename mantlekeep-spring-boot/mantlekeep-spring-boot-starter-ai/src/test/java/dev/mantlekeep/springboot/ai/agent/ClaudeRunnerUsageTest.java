package dev.mantlekeep.springboot.ai.agent;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

/**
 * Unit tests for per-call usage capture: the stream-json {@code result} parser, and the
 * {@link ClaudeRunner#stream(String, java.util.function.Consumer)} contract. No real
 * {@code claude} process is launched — the parser is exercised directly and the runner
 * through a canned stub.
 */
class ClaudeRunnerUsageTest {

    /** A representative terminating line, verbatim in the shape the CLI emits it. */
    private static final String RESULT_LINE = """
            {"type":"result","subtype":"success","duration_ms":1441,"result":"hi",\
            "usage":{"input_tokens":2,"cache_creation_input_tokens":16990,\
            "cache_read_input_tokens":14946,"output_tokens":13},"total_cost_usd":0.1777}""";

    private static final String DELTA_LINE = """
            {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}""";

    @Test
    void parsesTokensAndCostFromTheResultLine() {
        Usage usage = ProcessClaudeRunner.resultUsage(RESULT_LINE);

        assertThat(usage).isNotNull();
        assertThat(usage.inputTokens()).isEqualTo(2L);
        assertThat(usage.outputTokens()).isEqualTo(13L);
        assertThat(usage.cacheReadInputTokens()).isEqualTo(14946L);
        assertThat(usage.cacheCreationInputTokens()).isEqualTo(16990L);
        assertThat(usage.totalCostUsd()).isEqualTo(0.1777d);
        assertThat(usage.durationMillis()).isEqualTo(1441L);
        assertThat(usage.totalInputTokens()).isEqualTo(2L + 14946L + 16990L);
        assertThat(usage.isPresent()).isTrue();
    }

    @Test
    void ignoresLinesThatAreNotTheResultObject() {
        assertThat(ProcessClaudeRunner.resultUsage(DELTA_LINE)).isNull();
        assertThat(ProcessClaudeRunner.resultUsage("not json at all")).isNull();
    }

    @Test
    void missingUsageFieldsDegradeToZeroRatherThanFailing() {
        Usage usage = ProcessClaudeRunner.resultUsage("""
                {"type":"result","subtype":"success"}""");

        assertThat(usage).isEqualTo(Usage.NONE);
        assertThat(usage.isPresent()).isFalse();
    }

    @Test
    void aStreamWithoutAResultLineCompletesAndReportsNoUsage() {
        // The default ClaudeRunner has no result line to parse — the text still flows,
        // the sink is simply never invoked, and the caller reads that as Usage.NONE.
        ClaudeRunner runner = prompt -> Mono.just("drafted spec");
        List<Usage> reported = new ArrayList<>();

        StepVerifier.create(runner.stream("prompt", reported::add))
                .expectNext("drafted spec")
                .verifyComplete();

        assertThat(reported).isEmpty();
    }

    @Test
    void theTextOnlyOverloadStillStreamsUnchanged() {
        ClaudeRunner runner = prompt -> Mono.just("drafted spec");

        StepVerifier.create(runner.stream("prompt"))
                .expectNext("drafted spec")
                .verifyComplete();
    }
}
