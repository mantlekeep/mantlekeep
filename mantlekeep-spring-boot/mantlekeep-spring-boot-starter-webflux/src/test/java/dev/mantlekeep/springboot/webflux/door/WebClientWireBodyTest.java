package dev.mantlekeep.springboot.webflux.door;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.door.model.Subject;
import dev.mantlekeep.springboot.door.DoorProperties;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.time.Duration;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.web.reactive.function.client.WebClient;

/**
 * The outbound body must match the Go door's frozen contract exactly:
 * {@code {action, resource, goal, env, params}}. The reactive client builds this explicitly
 * from the spine's {@link Intent} rather than serialising the whole record, so neither the
 * record's {@code id} nor its {@code subject} (identity travels as a header, never the body)
 * leaks onto the wire. {@code env} rides in {@code params} and is lifted to the top-level
 * field the Go door reads. This test captures the real request body and pins it.
 */
class WebClientWireBodyTest {

    private HttpServer server;
    private final AtomicReference<String> lastBody = new AtomicReference<>("");

    @BeforeEach
    void startDoor() throws IOException {
        server = HttpServer.create(new InetSocketAddress(0), 0);
        server.createContext("/api/govern", exchange -> {
            lastBody.set(new String(exchange.getRequestBody().readAllBytes()));
            byte[] body = "{\"outcome\":\"allow\",\"token\":\"t\"}".getBytes();
            exchange.getResponseHeaders().set("Content-Type", "application/json");
            exchange.sendResponseHeaders(200, body.length);
            try (OutputStream out = exchange.getResponseBody()) {
                out.write(body);
            }
        });
        server.start();
    }

    @AfterEach
    void stopDoor() {
        server.stop(0);
    }

    private WebClientDoorClient client() {
        WebClient webClient = WebClient.builder()
                .baseUrl("http://localhost:" + server.getAddress().getPort())
                .build();
        DoorProperties properties = new DoorProperties("http://localhost:" + server.getAddress().getPort(),
                "/api/govern", "", "", "X-Mantlekeep-User", "X-Mantlekeep-On-Behalf-Of",
                Duration.ofSeconds(3), Duration.ofSeconds(10));
        return new WebClientDoorClient(webClient, properties);
    }

    @Test
    void theOutboundBodyMatchesTheCanonicalShapeAndCarriesNoIdentity() {
        Intent intent = new Intent("JINT-1", Subject.ofId("lead-bob"), "session.deploy", "s1",
                "deploy a session", Map.of("cpu", "4", "env", "PROD"));

        client().submit(intent).block(Duration.ofSeconds(5));

        String body = lastBody.get();
        assertFalse(body.contains("\"subject\"") || body.contains("\"JINT-1\"") || body.contains("lead-bob"),
                "identity must never be on the body — it travels as a header: " + body);
        assertTrue(body.contains("\"action\"") && body.contains("\"env\":\"PROD\"")
                        && body.contains("\"params\""),
                "the canonical fields must be present, env lifted to the top level: " + body);
    }
}
