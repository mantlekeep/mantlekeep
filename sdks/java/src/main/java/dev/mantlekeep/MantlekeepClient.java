package dev.mantlekeep;

import java.io.IOException;
import java.net.CookieManager;
import java.net.CookiePolicy;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;

/**
 * MantlekeepClient — a thin, dependency-free Java client for the MantleKeep core.
 *
 * <p>The core runs as a sidecar (a downloaded, signed binary); this class wraps its
 * HTTP "one door" so a Java app governs its own actions in one line. JDK 17+, and
 * <b>no external libraries</b> — only {@code java.net.http}.
 *
 * <pre>{@code
 *   var mantle = MantlekeepClient.connect("http://localhost:8080").login("lead-bob");
 *
 *   // guard style — throws MantlekeepDeniedException if the door says no:
 *   mantlekeep.govern(Intent.action("service.deploy").resource("txn/123").goal("deploy the service")).require();
 *   // ... proceed; the door allowed it and hash-chained the decision ...
 * }</pre>
 */
public final class MantlekeepClient {

    private final HttpClient http;
    private final String base;

    private MantlekeepClient(String base) {
        this.base = base.endsWith("/") ? base.substring(0, base.length() - 1) : base;
        this.http = HttpClient.newBuilder()
                .cookieHandler(new CookieManager(null, CookiePolicy.ACCEPT_ALL))
                .connectTimeout(Duration.ofSeconds(10))
                .build();
    }

    /** Connect to a running MantleKeep core (the sidecar URL, e.g. http://localhost:8080). */
    public static MantlekeepClient connect(String url) {
        return new MantlekeepClient(url);
    }

    /**
     * Dev-tier login (a session cookie). In production the app sits behind the host
     * SSO gateway, which sets the session for you — skip this and the gateway's
     * session flows through automatically.
     */
    public MantlekeepClient login(String user) {
        post("/api/login", "{\"user\":\"" + esc(user) + "\"}");
        return this;
    }

    /** Ask the one door to govern an action. Returns the decision (allow+token / deny). */
    public Decision govern(Intent intent) {
        String body = post("/api/govern", intent.toJson());
        boolean allow = "allow".equals(field(body, "decision"));
        return new Decision(allow, field(body, "token"), field(body, "reason"));
    }

    // ── minimal HTTP + JSON, no external deps ────────────────────────────────

    private String post(String path, String json) {
        try {
            HttpResponse<String> r = http.send(
                    HttpRequest.newBuilder(URI.create(base + path))
                            .header("Content-Type", "application/json")
                            .POST(HttpRequest.BodyPublishers.ofString(json))
                            .build(),
                    HttpResponse.BodyHandlers.ofString());
            return r.body();
        } catch (IOException transportFailure) {
            throw new MantlekeepException("MantleKeep call failed: " + path, transportFailure);
        } catch (InterruptedException interrupted) {
            // Restore the interrupt status the blocking send() cleared, then surface the failure —
            // swallowing the interrupt would strand a caller trying to cancel this thread.
            Thread.currentThread().interrupt();
            throw new MantlekeepException("MantleKeep call interrupted: " + path, interrupted);
        }
    }

    /** Extract a flat string field from the JSON response (a real SDK would use Jackson). */
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
