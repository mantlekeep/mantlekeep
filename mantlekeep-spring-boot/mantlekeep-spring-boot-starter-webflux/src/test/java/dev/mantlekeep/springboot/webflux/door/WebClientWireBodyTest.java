package dev.mantlekeep.springboot.webflux.door;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import dev.mantlekeep.springboot.door.DoorProperties;
import dev.mantlekeep.springboot.door.Intent;
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
 * {@code {action, resource, goal, env, params}}. The reactive client had been serialising
 * the whole {@link Intent} record via {@code bodyValue(intent)}, which also ships a
 * {@code scope} field the server silently drops — a latent divergence from the canonical
 * shape. This test captures the real request body and pins it.
 */
class WebClientWireBodyTest {

    private HttpServer server;
    private final AtomicReference<String> lastBody = new AtomicReference<>("");

    @BeforeEach
    void startDoor() throws IOException {
        server = HttpServer.create(new InetSocketAddress(0), 0);
        server.createContext("/api/govern", exchange -> {
            lastBody.set(new String(exchange.getRequestBody().readAllBytes()));
            byte[] body = "{\"decision\":\"allow\",\"token\":\"t\"}".getBytes();
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
    void theOutboundBodyMatchesTheCanonicalShapeAndOmitsScope() {
        Intent intent = Intent.of("session.deploy")
                .goal("deploy a session").resource("s1").env("PROD").scope("ignored")
                .params(Map.of("cpu", "4")).build();

        client().submit(intent).block(Duration.ofSeconds(5));

        String body = lastBody.get();
        assertFalse(body.contains("\"scope\""),
                "scope must not be on the wire — the Go server drops it: " + body);
        assertTrue(body.contains("\"action\"") && body.contains("\"env\"") && body.contains("\"params\""),
                "the canonical fields must be present: " + body);
    }
}
