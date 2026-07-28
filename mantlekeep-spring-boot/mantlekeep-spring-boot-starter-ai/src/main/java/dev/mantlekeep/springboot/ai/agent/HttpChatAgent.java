package dev.mantlekeep.springboot.ai.agent;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import dev.mantlekeep.springboot.agent.AgentPort;
import dev.mantlekeep.springboot.agent.LoopContext;
import dev.mantlekeep.springboot.agent.Role;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.List;
import java.util.Objects;
import java.util.stream.Stream;
import reactor.core.publisher.Flux;
import reactor.core.publisher.FluxSink;
import reactor.core.publisher.Mono;
import reactor.core.scheduler.Schedulers;

/**
 * A SERVER-SIDE {@link AgentPort} speaking the OpenAI-compatible chat-completions API.
 *
 * <p>This is the adapter that matters when an organisation cannot put a model CLI on the
 * server but does have a model endpoint: Azure OpenAI, a self-hosted vLLM or Ollama, or a
 * gateway like LiteLLM — they all speak this dialect. Point {@code url} at the full
 * chat-completions endpoint and the loop works with no editor, no developer acting as
 * transport, and the same prompts every other adapter uses ({@link Prompts}).
 *
 * <p>Deliberately built on the JDK's {@link HttpClient}: no new dependency enters the SDK for
 * what is, in the end, one POST. Blocking calls run on {@link Schedulers#boundedElastic()} so
 * the reactive chain is never pinned.
 *
 * <p>Auth is configurable because the dialect is not: OpenAI-style endpoints want
 * {@code Authorization: Bearer <key>}, Azure wants {@code api-key: <key>} with no prefix.
 */
public final class HttpChatAgent implements AgentPort {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private final URI url;
    private final String model;
    private final String apiKey;
    private final String authHeader;
    private final String authPrefix;
    private final Duration timeout;
    private final HttpClient client;

    public HttpChatAgent(URI url, String model, String apiKey, String authHeader,
            String authPrefix, Duration timeout) {
        this.url = Objects.requireNonNull(url, "url is required");
        this.model = Objects.requireNonNull(model, "model is required");
        this.apiKey = apiKey == null ? "" : apiKey;
        this.authHeader = (authHeader == null || authHeader.isBlank()) ? "Authorization" : authHeader;
        this.authPrefix = authPrefix == null ? "Bearer " : authPrefix;
        this.timeout = timeout == null ? Duration.ofSeconds(120) : timeout;
        this.client = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10)).build();
    }

    @Override
    public Mono<String> draft(Role role, LoopContext context) {
        Objects.requireNonNull(role, "role is required");
        Objects.requireNonNull(context, "context is required");
        return complete(Prompts.draft(role, context));
    }

    @Override
    public Flux<String> draftStream(Role role, LoopContext context) {
        Objects.requireNonNull(role, "role is required");
        Objects.requireNonNull(context, "context is required");
        return stream(Prompts.draft(role, context));
    }

    @Override
    public String identity() {
        return "http:" + model;
    }

    @Override
    public Mono<String> critique(LoopContext context, List<String> revisitableRoles) {
        Objects.requireNonNull(context, "context is required");
        if (revisitableRoles == null || revisitableRoles.isEmpty()) {
            return Mono.just("CONTINUE");
        }
        return complete(Prompts.critique(context, revisitableRoles));
    }

    /** One non-streaming completion. */
    private Mono<String> complete(String prompt) {
        return Mono.fromCallable(() -> {
            HttpResponse<String> response =
                    client.send(request(body(prompt, false)), HttpResponse.BodyHandlers.ofString());
            if (response.statusCode() / 100 != 2) {
                throw new IllegalStateException(
                        "model endpoint returned HTTP " + response.statusCode() + ": " + response.body());
            }
            return contentOf(response.body());
        }).subscribeOn(Schedulers.boundedElastic());
    }

    /** One streaming completion, emitting each content delta as it arrives. */
    private Flux<String> stream(String prompt) {
        return Flux.<String>create(sink -> {
            try {
                HttpResponse<Stream<String>> response =
                        client.send(request(body(prompt, true)), HttpResponse.BodyHandlers.ofLines());
                if (response.statusCode() / 100 != 2) {
                    sink.error(new IllegalStateException(
                            "model endpoint returned HTTP " + response.statusCode()));
                    return;
                }
                try (Stream<String> lines = response.body()) {
                    lines.forEach(line -> {
                        String delta = deltaOf(line);
                        if (delta != null && !delta.isEmpty()) {
                            sink.next(delta);
                        }
                    });
                }
                sink.complete();
            } catch (IOException | InterruptedException e) {
                sink.error(e);
            }
        }, FluxSink.OverflowStrategy.BUFFER).subscribeOn(Schedulers.boundedElastic());
    }

    private HttpRequest request(String body) {
        HttpRequest.Builder builder = HttpRequest.newBuilder(url)
                .timeout(timeout)
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body));
        if (!apiKey.isBlank()) {
            builder.header(authHeader, authPrefix + apiKey);
        }
        return builder.build();
    }

    /** Build the chat-completions request body. Package-private so it can be tested directly. */
    static String body(String model, String prompt, boolean stream) {
        ObjectNode root = MAPPER.createObjectNode();
        root.put("model", model);
        ObjectNode message = root.putArray("messages").addObject();
        message.put("role", "user");
        message.put("content", prompt);
        if (stream) {
            root.put("stream", true);
            // ask for usage on the final chunk so a caller can meter what the draft cost
            root.putObject("stream_options").put("include_usage", true);
        }
        try {
            return MAPPER.writeValueAsString(root);
        } catch (IOException e) {
            throw new IllegalStateException("could not build the model request", e);
        }
    }

    private String body(String prompt, boolean stream) {
        return body(model, prompt, stream);
    }

    /** The assistant text from a non-streaming response, or empty if the shape is unexpected. */
    static String contentOf(String responseBody) {
        try {
            JsonNode node = MAPPER.readTree(responseBody);
            return node.path("choices").path(0).path("message").path("content").asText("");
        } catch (IOException e) {
            return "";
        }
    }

    /**
     * The incremental text from one SSE line, or {@code null} when the line carries none —
     * comments, the terminating {@code [DONE]} sentinel, and the usage-only final chunk all
     * return null rather than emitting noise into the draft.
     */
    static String deltaOf(String line) {
        if (line == null || !line.startsWith("data:")) {
            return null;
        }
        String payload = line.substring("data:".length()).trim();
        if (payload.isEmpty() || "[DONE]".equals(payload)) {
            return null;
        }
        try {
            JsonNode choices = MAPPER.readTree(payload).path("choices");
            if (choices.isArray() && !choices.isEmpty()) {
                String content = choices.get(0).path("delta").path("content").asText("");
                return content.isEmpty() ? null : content;
            }
        } catch (IOException ignore) {
            // a non-JSON line — skip it rather than corrupting the draft
        }
        return null;
    }
}
