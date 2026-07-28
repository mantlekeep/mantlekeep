package dev.mantlekeep.springboot.ai.agent;

import java.util.Objects;
import java.util.function.Consumer;
import reactor.core.publisher.Flux;
import reactor.core.publisher.Mono;

/**
 * The seam between {@link HarnessAgent} and the actual Claude Code CLI invocation. A
 * functional interface so the agent can be unit-tested with a canned runner — no real
 * {@code claude} process. The default adapter is {@link ProcessClaudeRunner}.
 */
@FunctionalInterface
public interface ClaudeRunner {

    /**
     * Run the harness for the given prompt.
     *
     * @param prompt the fully-built prompt to hand to the CLI
     * @return a {@code Mono} emitting the harness's text output, or erroring if the run
     *         fails (non-zero exit, timeout, or launch failure)
     */
    Mono<String> run(String prompt);

    /**
     * Stream the harness output incrementally as it is produced, discarding the run's
     * measured cost. Kept as the zero-ceremony entry point for callers that only want text;
     * it delegates to {@link #stream(String, Consumer)} so there is exactly one streaming
     * implementation per adapter.
     *
     * @param prompt the fully-built prompt to hand to the CLI
     * @return a {@code Flux} emitting text chunks in order, completing when the run ends
     */
    default Flux<String> stream(String prompt) {
        return stream(prompt, usage -> { });
    }

    /**
     * Stream the harness output with the process running IN {@code workingDirectory}, so
     * anything it writes lands in a workspace the platform granted and can inspect.
     *
     * <p>Default: ignore the directory. A canned runner in a test has no process to place,
     * and an adapter that cannot honour it must not pretend to.
     */
    default Flux<String> stream(String prompt, java.nio.file.Path workingDirectory) {
        return stream(prompt);
    }

    /** As {@link #run(String)}, with the process running in {@code workingDirectory}. */
    default Mono<String> run(String prompt, java.nio.file.Path workingDirectory) {
        return run(prompt);
    }

    /**
     * Stream the harness output and hand the run's measured cost to {@code onUsage}.
     *
     * <p>The cost is delivered through a sink rather than mixed into the returned
     * {@code Flux} on purpose: the stream is the model's <em>text</em>, and callers pipe it
     * straight to an SSE response or a token buffer. Widening the element type (or appending
     * a sentinel chunk) would force every existing caller to filter, which the sealed-SDK
     * rule forbids. A callback keeps the text channel pure and leaves the metering channel
     * out of band, so a caller opts in by passing one lambda and changes nothing else.
     *
     * <p>{@code onUsage} is invoked at most once per subscription, on the thread draining the
     * process's stdout, before the {@code Flux} completes. It is not invoked when the run
     * errors before the CLI reports usage — treat "not invoked" as {@link Usage#NONE}. It
     * must not throw and must not block.
     *
     * <p>The default adapts {@link #run} into a single-element stream and never reports usage
     * (a plain {@code Mono} runner has none to report); {@link ProcessClaudeRunner} overrides
     * it with real token-by-token streaming and real measured cost.
     *
     * @param prompt  the fully-built prompt to hand to the CLI
     * @param onUsage receives the run's measured token counts and cost, at most once
     * @return a {@code Flux} emitting text chunks in order, completing when the run ends
     */
    default Flux<String> stream(String prompt, Consumer<Usage> onUsage) {
        Objects.requireNonNull(onUsage, "onUsage is required");
        return run(prompt).flux();
    }
}
