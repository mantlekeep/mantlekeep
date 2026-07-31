package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;

import com.sun.net.httpserver.HttpServer;
import dev.mantlekeep.door.model.DoorConfig;
import dev.mantlekeep.door.model.DoorMode;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.door.model.Subject;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URI;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

/**
 * The client must carry WHO IS ACTING on the wire, or the door refuses every request with
 * "unauthenticated: no caller identity".
 *
 * <p>This was broken: {@code decide} sent action, resource, goal and params, silently
 * dropped {@code Intent.subject}, and set no identity header. It worked only where a dev
 * login cookie happened to exist — so it passed in development and failed everywhere real.
 *
 * <p>These tests inspect the actual HTTP request rather than the client's return value,
 * because the defect was invisible from the caller's side: the client behaved correctly and
 * the request was incomplete.
 */
class ServiceDoorClientIdentityTest {

    private HttpServer server;
    private final Map<String, String> lastHeaders = new ConcurrentHashMap<>();
    private volatile String lastBody = "";

    @BeforeEach
    void startDoor() throws IOException {
        server = HttpServer.create(new InetSocketAddress(0), 0);
        server.createContext("/api/govern", exchange -> {
            lastHeaders.clear();
            exchange.getRequestHeaders()
                    .forEach((name, values) -> lastHeaders.put(name, String.join(",", values)));
            lastBody = new String(exchange.getRequestBody().readAllBytes());
            byte[] response = "{\"decision\":\"allow\",\"token\":\"t\"}".getBytes();
            exchange.sendResponseHeaders(200, response.length);
            try (OutputStream out = exchange.getResponseBody()) {
                out.write(response);
            }
        });
        server.start();
    }

    @AfterEach
    void stopDoor() {
        server.stop(0);
    }

    private URI doorUrl() {
        return URI.create("http://localhost:" + server.getAddress().getPort());
    }

    private static Intent intentBy(String subjectId) {
        return new Intent("", new Subject(subjectId, "operator"), "session.deploy",
                "session-a", "deploy a session", Map.of());
    }

    @Test
    void withoutAServiceIdentityTheSubjectIsTheCaller() {
        try (DoorClient door = new ServiceDoorClient(doorUrl(), "", "",
                "X-Mantlekeep-User", "X-Mantlekeep-On-Behalf-Of")) {
            door.decide(intentBy("lead-bob"));
        }

        assertEquals("lead-bob", lastHeaders.get("X-mantlekeep-user"),
                "the intent's subject must reach the door as the caller; without it the "
                        + "door sees nobody and refuses");
    }

    @Test
    void withAServiceIdentityTheServiceIsTheCallerAndThePersonIsNamedSeparately() {
        try (DoorClient door = new ServiceDoorClient(doorUrl(), "", "session-service",
                "X-Mantlekeep-User", "X-Mantlekeep-On-Behalf-Of")) {
            door.decide(intentBy("lead-bob"));
        }

        assertEquals("session-service", lastHeaders.get("X-mantlekeep-user"),
                "the application authenticates as itself");
        assertEquals("lead-bob", lastHeaders.get("X-mantlekeep-on-behalf-of"),
                "and names the person it acts for — both belong on the record");
    }

    @Test
    void headerNamesFollowTheBrand() {
        // A white-labelled product renames them; a door reading X-Acme-User will not see
        // X-Mantlekeep-User, and the mismatch surfaces as an unexplained 401.
        try (DoorClient door = new ServiceDoorClient(doorUrl(), "", "session-service",
                "X-Acme-User", "X-Acme-On-Behalf-Of")) {
            door.decide(intentBy("lead-bob"));
        }

        assertEquals("session-service", lastHeaders.get("X-acme-user"));
        assertEquals("lead-bob", lastHeaders.get("X-acme-on-behalf-of"));
        assertNull(lastHeaders.get("X-mantlekeep-user"), "the framework's name must not leak through");
    }

    @Test
    void configuredHeaderNamesSurviveTheFACTORY_notJustTheConstructor() {
        // The gap this covers: the client honoured header names passed to its constructor,
        // and DoorConfig carried them, but DoorClientFactory dropped them in between — so
        // a configured deployment silently sent the framework's default names. Constructing
        // the client directly could never reveal that; only building it the way a product
        // does, from configuration, can.
        DoorConfig config = new DoorConfig(DoorMode.SERVICE, null, doorUrl(), null, null, null, null,
                "session-service", "X-Acme-User", "X-Acme-On-Behalf-Of");

        try (DoorClient door = DoorClientFactory.create(config)) {
            door.decide(intentBy("lead-bob"));
        }

        assertEquals("session-service", lastHeaders.get("X-acme-user"),
                "the CONFIGURED caller header must reach the wire, not the framework default");
        assertEquals("lead-bob", lastHeaders.get("X-acme-on-behalf-of"));
        assertNull(lastHeaders.get("X-mantlekeep-user"),
                "falling back to the default name is the bug: the door reads X-Acme-User "
                        + "and would refuse this with an unexplained 401");
    }

    @Test
    void theSubjectNeverTravelsInTheBody() {
        try (DoorClient door = new ServiceDoorClient(doorUrl(), "", "session-service",
                "X-Mantlekeep-User", "X-Mantlekeep-On-Behalf-Of")) {
            door.decide(intentBy("lead-bob"));
        }

        // A body-supplied caller is forgeable, which would defeat resolving roles
        // server-side. Identity belongs in headers something in front of the door controls.
        assertFalse(lastBody.contains("lead-bob"),
                "the subject must NOT be in the request body: " + lastBody);
    }
}
