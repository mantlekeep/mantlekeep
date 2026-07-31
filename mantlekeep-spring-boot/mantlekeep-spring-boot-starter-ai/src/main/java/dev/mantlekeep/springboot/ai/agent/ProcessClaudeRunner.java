package dev.mantlekeep.springboot.ai.agent;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.BufferedReader;
import java.io.IOException;
import java.nio.file.Path;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.List;
import java.util.Objects;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.Executor;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import reactor.core.publisher.Flux;
import reactor.core.publisher.FluxSink;
import reactor.core.publisher.Mono;
import reactor.core.scheduler.Schedulers;

/**
 * The default {@link ClaudeRunner}: shells out to the Claude Code CLI headless via
 * {@code <binary> -p "<prompt>" --output-format text}. Blocking process work runs on
 * {@link Schedulers#boundedElastic()} so the reactive chain is never pinned.
 *
 * <p>stdout and stderr are drained on separate virtual threads to avoid the classic
 * pipe-buffer deadlock. The run is bounded by a timeout; on timeout the process is
 * forcibly destroyed. A non-zero exit, a timeout, or a launch failure error the
 * {@code Mono} with the captured stderr for diagnosis.
 */
public final class ProcessClaudeRunner implements ClaudeRunner {

    /** Virtual-thread-per-task: cheap, and stream draining is inherently blocking. */
    private static final Executor DRAIN = Executors.newVirtualThreadPerTaskExecutor();

    private final String binary;
    private final Duration timeout;

    /**
     * @param binary  the CLI binary to launch (e.g. {@code claude})
     * @param timeout how long a single run may take before it is killed
     */
    public ProcessClaudeRunner(String binary, Duration timeout) {
        this.binary = Objects.requireNonNull(binary, "binary is required");
        this.timeout = Objects.requireNonNull(timeout, "timeout is required");
    }

    /** A shared, thread-safe JSON reader for the CLI's stream-json output. */
    private static final ObjectMapper MAPPER = new ObjectMapper();

    @Override
    public Mono<String> run(String prompt) {
        Objects.requireNonNull(prompt, "prompt is required");
        return Mono.fromCallable(() -> invoke(prompt))
                .subscribeOn(Schedulers.boundedElastic());
    }

    /**
     * Stream the answer token-by-token and report what the run cost: launch the CLI in
     * {@code stream-json} mode with partial messages, emit each {@code text_delta} chunk as
     * it arrives, and hand the terminating {@code result} line's usage to {@code onUsage}.
     * stderr is drained concurrently; a timeout forcibly destroys the process; a non-zero
     * exit errors the {@code Flux}. Disposing the subscription kills the process.
     *
     * <p>Usage is reported at most once, before completion, and only if the CLI actually
     * emitted a result line — a launch failure, a timeout, or a crash mid-stream leaves
     * {@code onUsage} uncalled, because there is no measured cost to report.
     */
    @Override
    public Flux<String> stream(String prompt, Consumer<Usage> onUsage) {
        return stream(prompt, onUsage, null);
    }

    /**
     * Stream with the harness running IN {@code workingDirectory}, so files it writes land in
     * the workspace the platform granted rather than wherever this JVM happens to sit.
     */
    @Override
    public Flux<String> stream(String prompt, Path workingDirectory) {
        return stream(prompt, usage -> { }, workingDirectory);
    }

    private Flux<String> stream(String prompt, Consumer<Usage> onUsage, Path workingDirectory) {
        Objects.requireNonNull(prompt, "prompt is required");
        Objects.requireNonNull(onUsage, "onUsage is required");
        return Flux.<String>create(sink -> {
            Process process;
            try {
                List<String> command = new java.util.ArrayList<>(List.of(binary, "-p", prompt,
                        "--output-format", "stream-json", "--include-partial-messages", "--verbose"));
                if (workingDirectory != null) {
                    // A granted workspace must also be a granted PERMISSION. The harness runs
                    // with no write access by default, so without this the agent cannot write
                    // where MantleKeep just told it to — it produces prose about the work instead,
                    // and the step fails for a reason it had no way to avoid.
                    //
                    // Two permission systems meet here, and this is the translation. It is
                    // deliberately the smallest one that works: Write and Edit only, never
                    // Bash, and never --dangerously-skip-permissions. The DIRECTORY is the
                    // boundary — the process is placed inside the workspace, so the tools it
                    // gains reach only what was granted.
                    command.addAll(List.of("--allowedTools", "Write,Edit"));
                }
                ProcessBuilder builder = new ProcessBuilder(command);
                if (workingDirectory != null) {
                    builder.directory(workingDirectory.toFile());
                }
                process = builder.start();
            } catch (IOException e) {
                sink.error(e);
                return;
            }
            sink.onDispose(process::destroyForcibly);
            CompletableFuture<String> stderr = drain(process.getErrorStream());
            DRAIN.execute(() -> {
                try (var reader = new BufferedReader(
                        new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8))) {
                    String line;
                    Usage usage = null;
                    while ((line = reader.readLine()) != null) {
                        String text = textDelta(line);
                        if (text != null && !text.isEmpty()) {
                            sink.next(text);
                            continue; // a delta line never carries usage — skip the reparse
                        }
                        if (usage == null) {
                            usage = resultUsage(line);
                        }
                    }
                    if (usage != null) {
                        onUsage.accept(usage);
                    }
                    if (!process.waitFor(timeout.toMillis(), TimeUnit.MILLISECONDS)) {
                        process.destroyForcibly();
                        process.waitFor();
                        sink.error(new IllegalStateException(
                                "claude CLI timed out after " + timeout + " (binary: " + binary + ")"));
                        return;
                    }
                    int exit = process.exitValue();
                    if (exit != 0) {
                        sink.error(new IllegalStateException(
                                "claude CLI exited " + exit + ": " + stderr.join().strip()));
                        return;
                    }
                    sink.complete();
                } catch (IOException | InterruptedException e) {
                    process.destroyForcibly();
                    sink.error(e);
                }
            });
        }, FluxSink.OverflowStrategy.BUFFER);
    }

    /**
     * The incremental answer text from one stream-json line, or {@code null} if the line is
     * not answer text. Emits only {@code text_delta} content — skips thinking, tool, hook and
     * system lines — tolerating both the top-level and {@code stream_event}-wrapped shapes.
     */
    private static String textDelta(String line) {
        try {
            JsonNode node = MAPPER.readTree(line);
            JsonNode delta = null;
            if ("content_block_delta".equals(node.path("type").asText())) {
                delta = node.path("delta");
            } else if ("stream_event".equals(node.path("type").asText())
                    && "content_block_delta".equals(node.path("event").path("type").asText())) {
                delta = node.path("event").path("delta");
            }
            if (delta != null && "text_delta".equals(delta.path("type").asText())) {
                return delta.path("text").asText("");
            }
        } catch (IOException ignore) {
            // a non-JSON line (unexpected with stream-json) — skip it
        }
        return null;
    }

    /**
     * The measured cost carried by one stream-json line, or {@code null} if the line is not
     * the terminating {@code result} object.
     *
     * <p>Every field is read defensively: {@code path(...)} plus a typed default means a CLI
     * version that renames or drops a counter degrades to a zero in that column rather than
     * failing the whole run — a metering signal is worth less than the draft it measures, so
     * it must never be the thing that breaks the loop.
     */
    static Usage resultUsage(String line) {
        try {
            JsonNode node = MAPPER.readTree(line);
            if (!"result".equals(node.path("type").asText())) {
                return null;
            }
            JsonNode usage = node.path("usage");
            return new Usage(
                    usage.path("input_tokens").asLong(0L),
                    usage.path("output_tokens").asLong(0L),
                    usage.path("cache_read_input_tokens").asLong(0L),
                    usage.path("cache_creation_input_tokens").asLong(0L),
                    node.path("total_cost_usd").asDouble(0.0d),
                    node.path("duration_ms").asLong(0L));
        } catch (IOException ignore) {
            // a non-JSON line (unexpected with stream-json) — it carries no usage
            return null;
        }
    }

    private String invoke(String prompt) throws IOException, InterruptedException {
        List<String> command = List.of(binary, "-p", prompt, "--output-format", "text");
        Process process = new ProcessBuilder(command).start();

        // Drain both streams concurrently — reading inline can deadlock on a full pipe.
        CompletableFuture<String> stdout = drain(process.getInputStream());
        CompletableFuture<String> stderr = drain(process.getErrorStream());

        if (!process.waitFor(timeout.toMillis(), TimeUnit.MILLISECONDS)) {
            process.destroyForcibly();
            process.waitFor(); // reap the killed process
            throw new IllegalStateException(
                    "claude CLI timed out after " + timeout + " (binary: " + binary + ")");
        }

        int exit = process.exitValue();
        if (exit != 0) {
            throw new IllegalStateException(
                    "claude CLI exited " + exit + ": " + stderr.join().strip());
        }
        return stdout.join().strip();
    }

    private static CompletableFuture<String> drain(InputStream stream) {
        return CompletableFuture.supplyAsync(() -> {
            try (stream) {
                return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
            } catch (IOException e) {
                throw new UncheckedIOException(e);
            }
        }, DRAIN);
    }
}
