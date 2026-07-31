package dev.mantlekeep.door;

import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import java.net.CookieManager;
import java.net.CookiePolicy;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * The service mode: the remote door over its versioned wire contract — production,
 * scalable, ONE shared chain. Pure JDK {@code java.net.http}; the door's language
 * is invisible.
 *
 * <p>Wire (the same contract the portal serves):
 * <ul>
 *   <li>{@code POST /api/govern} — {@code {action, resource, goal, env, params}} →
 *       {@code {"decision":"allow","token":…}} or {@code {"decision":"deny","reason":…}}</li>
 *   <li>{@code GET /api/audit} — {@code {"intact":bool,"count":n,"records":[…]}}</li>
 * </ul>
 *
 * <p>Identity: in production the app sits behind the SSO gateway, whose session flows
 * through this client's cookie handler automatically. {@code devLoginUser} (dev tier
 * only) performs the door's {@code POST /api/login} once, lazily, before the first call.
 */
public final class ServiceDoorClient implements DoorClient {

    private static final String ENVIRONMENT_PARAMETER = "env";

    private final URI doorUrl;
    private final String devLoginUser;
    private final HttpClient httpClient;
    private final AtomicBoolean devLoginDone = new AtomicBoolean();
    private final String serviceUser;
    private final String callerHeader;
    private final String onBehalfOfHeader;

    /** Development convenience: no service identity, cookie login only. */
    public ServiceDoorClient(URI doorUrl, String devLoginUser) {
        this(doorUrl, devLoginUser, "", "X-Mantlekeep-User", "X-Mantlekeep-On-Behalf-Of");
    }

    public ServiceDoorClient(URI doorUrl, String devLoginUser, String serviceUser,
            String callerHeader, String onBehalfOfHeader) {
        this.serviceUser = serviceUser == null ? "" : serviceUser.trim();
        this.callerHeader = callerHeader;
        this.onBehalfOfHeader = onBehalfOfHeader;
        this.doorUrl = stripTrailingSlash(doorUrl);
        this.devLoginUser = devLoginUser == null ? "" : devLoginUser;
        this.httpClient = HttpClient.newBuilder()
                .cookieHandler(new CookieManager(null, CookiePolicy.ACCEPT_ALL))
                .connectTimeout(Duration.ofSeconds(10))
                .build();
    }

    @Override
    public Decision decide(Intent intent) {
        loginForDevelopmentIfConfigured();
        // env is read generically off params — the engine names no environment. It is a
        // scalar by convention, so render whatever was supplied rather than assuming.
        Object environmentValue = intent.parameters().get(ENVIRONMENT_PARAMETER);
        String environment = environmentValue == null ? "" : environmentValue.toString();
        String governRequestJson = "{"
                + "\"action\":" + JsonText.quote(intent.action()) + ","
                + "\"resource\":" + JsonText.quote(intent.resource()) + ","
                + "\"goal\":" + JsonText.quote(intent.goal()) + ","
                + "\"env\":" + JsonText.quote(environment) + ","
                + "\"params\":" + JsonText.object(intent.parameters())
                + "}";
        String responseJson = post("/api/govern", governRequestJson, intent.subject().id());
        boolean allowed = "allow".equals(JsonText.stringField(responseJson, "decision"));
        return allowed
                ? Decision.allow(JsonText.stringField(responseJson, "token"))
                : Decision.deny(JsonText.stringField(responseJson, "reason"));
    }

    @Override
    public List<AuditRecord> audit() {
        String auditViewJson = get("/api/audit");
        return JsonText.objectsOfArray(JsonText.arrayField(auditViewJson, "records")).stream()
                .map(recordJson -> new AuditRecord(
                        "", // the wire view deliberately omits internals like the intent id
                        JsonText.stringField(recordJson, "subject"),
                        JsonText.stringField(recordJson, "action"),
                        JsonText.stringField(recordJson, "decision"),
                        recordJson))
                .toList();
    }

    @Override
    public boolean verify() {
        return JsonText.booleanField(get("/api/audit"), "intact", false);
    }

    @Override
    public void close() {
        // JDK HttpClient holds no external resources needing explicit release here.
    }

    private void loginForDevelopmentIfConfigured() {
        if (!devLoginUser.isBlank() && devLoginDone.compareAndSet(false, true)) {
            post("/api/login", "{\"user\":" + JsonText.quote(devLoginUser) + "}");
        }
    }

    private String post(String path, String requestJson) {
        return post(path, requestJson, "");
    }

    /**
     * Sends the request with the caller carried in HEADERS, never in the body.
     *
     * <p>The subject travels as a header because a body-supplied caller is forgeable: the
     * door resolves roles server-side from an identity something in front of it
     * authenticated, and a request that could name its own subject would make that
     * pointless.
     *
     * <p>When this application has its own identity, it authenticates as itself and names
     * the person separately — recorded as the subject, with the service as {@code via}.
     * Without one, the subject is the caller.
     */
    private String post(String path, String requestJson, String subjectId) {
        HttpRequest.Builder request = HttpRequest.newBuilder(URI.create(doorUrl + path))
                .header("Content-Type", "application/json");
        if (!serviceUser.isBlank()) {
            request.header(callerHeader, serviceUser);
            if (!subjectId.isBlank() && !subjectId.equals(serviceUser)) {
                request.header(onBehalfOfHeader, subjectId);
            }
        } else if (!subjectId.isBlank()) {
            request.header(callerHeader, subjectId);
        }
        return send(request.POST(HttpRequest.BodyPublishers.ofString(requestJson)).build());
    }

    private String get(String path) {
        HttpRequest.Builder request = HttpRequest.newBuilder(URI.create(doorUrl + path));
        if (!serviceUser.isBlank()) {
            request.header(callerHeader, serviceUser);
        }
        return send(request.GET().build());
    }

    private String send(HttpRequest request) {
        try {
            HttpResponse<String> response =
                    httpClient.send(request, HttpResponse.BodyHandlers.ofString());
            return response.body();
        } catch (Exception failure) {
            if (failure instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            throw new IllegalStateException(
                    "door call failed: " + request.method() + " " + request.uri(), failure);
        }
    }

    private static URI stripTrailingSlash(URI url) {
        String text = url.toString();
        return text.endsWith("/") ? URI.create(text.substring(0, text.length() - 1)) : url;
    }
}
