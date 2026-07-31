package dev.mantlekeep.client;

import java.net.CookieManager;
import java.net.CookiePolicy;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * SessionClient — the dependency-free Java client a CONTROL PLANE uses to drive the
 * session-lifecycle service. It wraps the service's REST boundary
 * ({@code :deploy/:update/:save/:kill}) and the async long-running-operation shape: a mutating
 * call returns an {@link OperationHandle} immediately; {@link #await} polls until the operation
 * reaches a terminal state.
 *
 * <p><b>Governance is NOT here</b> — it lives in the service (every order passes the door, and the
 * outcome is the service's source of truth). This is a thin, typed transport client so a control
 * plane integrates in a few lines instead of hand-rolling HTTP + the poll loop. JDK 17+, and no
 * external libraries — only {@code java.net.http}.
 *
 * <pre>{@code
 *   var sessions = SessionClient.connect("http://localhost:8092").as("ml-platform");
 *   var op = sessions.deploy(SessionClient.deploy("sess-1", "dev-a")
 *              .namespace("team-x").image("harbor/img@sha256:bb...").maxCpu("8").build());
 *   var done = sessions.await(op, Duration.ofMinutes(2));   // poll until terminal
 *   if (!done.succeeded()) throw new IllegalStateException(done.detail());
 * }</pre>
 */
public final class SessionClient {

    private final HttpClient http;
    private final String base;
    private String caller = "anonymous";

    private SessionClient(String base) {
        this.base = base.endsWith("/") ? base.substring(0, base.length() - 1) : base;
        this.http = HttpClient.newBuilder()
                .cookieHandler(new CookieManager(null, CookiePolicy.ACCEPT_ALL))
                .connectTimeout(Duration.ofSeconds(10))
                .build();
    }

    /** Connect to a running session service (e.g. http://localhost:8092). */
    public static SessionClient connect(String url) {
        return new SessionClient(url);
    }

    /**
     * The control plane's caller identity, propagated to the service so it records WHO really
     * acted (as on-behalf-of on the door's chain). Sent as a header; behind the host SSO gateway
     * the session flows automatically and this is effectively a no-op.
     */
    public SessionClient as(String caller) {
        this.caller = caller == null ? "anonymous" : caller;
        return this;
    }

    // ── mutating verbs → an async handle ──────────────────────────────────────

    public OperationHandle deploy(DeploySpec spec) {
        return handle(post("/:deploy", spec.toJson()));
    }

    public OperationHandle update(DeploySpec spec) {
        return handle(post("/:update", spec.toJson()));
    }

    public OperationHandle save(String sessionId, String clusterId, String saveMode) {
        return handle(post("/:save", obj(Map.of(
                "sessionId", sessionId, "clusterId", clusterId,
                "saveMode", saveMode == null ? "snapshot" : saveMode))));
    }

    public OperationHandle kill(String sessionId, String clusterId, boolean force) {
        return handle("{\"sessionId\":\"" + esc(sessionId) + "\",\"clusterId\":\""
                + esc(clusterId) + "\",\"force\":" + force + "}", "/:kill");
    }

    // ── the long-running-operation contract ───────────────────────────────────

    /** Poll one operation's current state — the service's source of truth for what happened. */
    public OperationHandle poll(String operationId) {
        return handle(get("/operations/" + operationId));
    }

    /**
     * Poll until the operation is terminal (succeeded / failed) or the timeout elapses. This is the
     * convenience a control plane wants: submit, then block until the outcome is known, without
     * re-implementing the poll loop. A real integration may prefer a callback instead of polling.
     */
    public OperationHandle await(OperationHandle op, Duration timeout) {
        long deadlineNanos = System.nanoTime() + timeout.toNanos();
        OperationHandle current = op;
        while (!current.isTerminal()) {
            if (System.nanoTime() > deadlineNanos) {
                throw new SessionClientException("operation " + current.operationId()
                        + " did not finish within " + timeout);
            }
            sleep(500);
            current = poll(current.operationId());
        }
        return current;
    }

    // ── the deploy request builder (the generic shape; a caller's exact fields stay theirs) ──

    public static DeploySpec deploy(String sessionId, String clusterId) {
        return new DeploySpec(sessionId, clusterId);
    }

    /** Fluent builder for a deploy/update order. Only the non-secret plan metadata — secret Helm
     *  values are brokered by the service, never sent here. */
    public static final class DeploySpec {
        private final String sessionId;
        private final String clusterId;
        private String userId = "";
        private String projectId = "";
        private String namespace = "default";
        private String imageRef = "";
        private String helmChartRef = "";
        private String callbackUrl = "";
        private final Map<String, String> maxResources = new LinkedHashMap<>();

        private DeploySpec(String sessionId, String clusterId) {
            this.sessionId = sessionId;
            this.clusterId = clusterId;
        }

        public DeploySpec user(String userId) { this.userId = userId; return this; }
        public DeploySpec project(String projectId) { this.projectId = projectId; return this; }
        public DeploySpec namespace(String namespace) { this.namespace = namespace; return this; }
        public DeploySpec image(String imageRef) { this.imageRef = imageRef; return this; }
        public DeploySpec chart(String helmChartRef) { this.helmChartRef = helmChartRef; return this; }
        public DeploySpec maxCpu(String cpu) { maxResources.put("cpu", cpu); return this; }
        public DeploySpec maxMemory(String mem) { maxResources.put("memory", mem); return this; }

        /** Ask to be POSTed the terminal operation at this url instead of polling. Optional. */
        public DeploySpec callback(String url) { this.callbackUrl = url == null ? "" : url; return this; }

        public DeploySpec build() { return this; } // reads nicer in the fluent chain

        String toJson() {
            StringBuilder b = new StringBuilder("{");
            kv(b, "sessionId", sessionId).append(',');
            kv(b, "userId", userId).append(',');
            kv(b, "projectId", projectId).append(',');
            kv(b, "clusterId", clusterId).append(',');
            kv(b, "namespace", namespace).append(',');
            kv(b, "imageRef", imageRef).append(',');
            kv(b, "helmChartRef", helmChartRef).append(',');
            kv(b, "callbackUrl", callbackUrl).append(',');
            b.append("\"maxResources\":").append(mapJson(maxResources));
            return b.append('}').toString();
        }
    }

    /** The async handle: the operation id to poll, its status, and a human detail (a deny reason
     *  on refusal). Terminal statuses are {@code succeeded}, {@code failed}, and {@code denied}
     *  (a governance refusal — no server operation exists; {@code operationId} is null and the reason
     *  is in {@code detail}). Treating {@code denied} as terminal is what stops {@link #await} from
     *  polling a refused, id-less order forever. */
    public record OperationHandle(String operationId, String status, String detail) {
        public boolean isTerminal() {
            return "succeeded".equalsIgnoreCase(status) || "failed".equalsIgnoreCase(status)
                    || "denied".equalsIgnoreCase(status);
        }

        public boolean succeeded() {
            return "succeeded".equalsIgnoreCase(status);
        }
    }

    /** Thrown when the client cannot reach the service or an operation does not complete in time.
     *  A GOVERNANCE denial is NOT an exception — it surfaces as a terminal handle with status
     *  {@code denied} and the reason in {@link OperationHandle#detail()}. */
    public static final class SessionClientException extends RuntimeException {
        public SessionClientException(String message) { super(message); }
        public SessionClientException(String message, Throwable cause) { super(message, cause); }
    }

    // ── minimal HTTP + JSON, no external deps (a real SDK would use Jackson) ──

    private OperationHandle handle(String body, String path) {
        return handle(post(path, body));
    }

    private OperationHandle handle(String responseBody) {
        return new OperationHandle(
                field(responseBody, "operationId"),
                field(responseBody, "status"),
                field(responseBody, "detail"));
    }

    private String post(String path, String json) {
        return send(HttpRequest.newBuilder(URI.create(base + "/api/sessions" + path))
                .header("Content-Type", "application/json")
                .header("X-Mantlekeep-User", caller)
                .POST(HttpRequest.BodyPublishers.ofString(json)), path);
    }

    private String get(String path) {
        return send(HttpRequest.newBuilder(URI.create(base + "/api/sessions" + path))
                .header("X-Mantlekeep-User", caller)
                .GET(), path);
    }

    private String send(HttpRequest.Builder req, String path) {
        try {
            HttpResponse<String> response = http.send(req.build(), HttpResponse.BodyHandlers.ofString());
            return response.body();
        } catch (Exception e) {
            throw new SessionClientException("session call failed: " + path, e);
        }
    }

    private static void sleep(long millis) {
        try {
            Thread.sleep(millis);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new SessionClientException("interrupted while awaiting operation", e);
        }
    }

    private static String obj(Map<String, String> m) {
        return mapJson(m);
    }

    private static String mapJson(Map<String, String> m) {
        StringBuilder b = new StringBuilder("{");
        boolean first = true;
        for (Map.Entry<String, String> e : m.entrySet()) {
            if (!first) b.append(',');
            kv(b, e.getKey(), e.getValue());
            first = false;
        }
        return b.append('}').toString();
    }

    private static StringBuilder kv(StringBuilder b, String k, String v) {
        return b.append('"').append(esc(k)).append("\":\"").append(esc(v)).append('"');
    }

    private static String field(String json, String key) {
        if (json == null) return null;
        String needle = "\"" + key + "\":\"";
        int i = json.indexOf(needle);
        if (i < 0) return null;
        i += needle.length();
        int j = json.indexOf('"', i);
        return j < 0 ? null : json.substring(i, j);
    }

    private static String esc(String s) {
        return s == null ? "" : s.replace("\\", "\\\\").replace("\"", "\\\"");
    }
}
