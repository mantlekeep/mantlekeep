package dev.mantlekeep.springboot.webflux.door;

import static org.junit.jupiter.api.Assertions.assertEquals;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import dev.mantlekeep.door.DoorClient;
import dev.mantlekeep.door.ServiceDoorClient;
import dev.mantlekeep.door.model.DoorConfig;
import dev.mantlekeep.door.model.DoorMode;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.door.model.Subject;
import dev.mantlekeep.springboot.door.DoorProperties;
import dev.mantlekeep.springboot.door.OnBehalfOf;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.web.reactive.function.client.WebClient;

/**
 * The test that prevents the two door clients from ever diverging again
 * (java-consolidation.md §5). It builds the BLOCKING spine client
 * ({@link ServiceDoorClient}) and the REACTIVE adapter ({@link WebClientDoorClient}) from
 * the SAME {@link DoorConfig}, submits the SAME {@link Intent} through each to a recording
 * server, and asserts the method, path, the door-relevant headers, and the body are
 * identical.
 *
 * <p>This is the guard the consolidation note calls for: nothing else in the repo compares
 * the two paths, so nothing else notices when one reaches the wire differently from the
 * other — which is the only difference a door can observe. Every historical divergence
 * defect (a header sent by one client and dropped by the other, a body field one shipped and
 * the other did not) fails this test the moment it reappears.
 */
class CrossPathWireEqualityTest {

    /** One captured request, reduced to what the door actually sees. */
    private record CapturedRequest(String method, String path, String caller, String onBehalfOf,
            String contentType, String body) {
    }

    private HttpServer door;
    private final List<CapturedRequest> captured = new ArrayList<>();

    private static final String CALLER_HEADER = "X-Mantlekeep-User";
    private static final String ON_BEHALF_OF_HEADER = "X-Mantlekeep-On-Behalf-Of";

    @BeforeEach
    void startDoor() throws IOException {
        door = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        door.createContext("/api/govern", exchange -> {
            captured.add(capture(exchange));
            byte[] body = "{\"outcome\":\"allow\",\"token\":\"t\",\"reasons\":[]}"
                    .getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().set("Content-Type", "application/json");
            exchange.sendResponseHeaders(200, body.length);
            try (OutputStream out = exchange.getResponseBody()) {
                out.write(body);
            }
        });
        door.start();
    }

    @AfterEach
    void stopDoor() {
        door.stop(0);
    }

    private static CapturedRequest capture(HttpExchange exchange) throws IOException {
        String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
        return new CapturedRequest(
                exchange.getRequestMethod(),
                exchange.getRequestURI().getPath(),
                exchange.getRequestHeaders().getFirst(CALLER_HEADER),
                exchange.getRequestHeaders().getFirst(ON_BEHALF_OF_HEADER),
                exchange.getRequestHeaders().getFirst("Content-Type"),
                body);
    }

    @Test
    void bothClientsReachTheWireIdenticallyFromOneConfigAndOneIntent() {
        URI doorUrl = URI.create("http://127.0.0.1:" + door.getAddress().getPort());
        // ONE config: a service that authenticates as itself and acts for a person.
        DoorConfig config = new DoorConfig(DoorMode.SERVICE, null, doorUrl, null, null, null, null,
                "session-service", CALLER_HEADER, ON_BEHALF_OF_HEADER);
        // ONE intent: a single-key params map so the serialised body is order-deterministic.
        Intent intent = new Intent("", Subject.ofId("lead-bob"), "job.promote", "project/demo",
                "ship 1.2", Map.of("env", "PROD"));

        blockingClient(config).decide(intent);
        reactiveClient(config).submit(intent)
                .contextWrite(OnBehalfOf.with("lead-bob"))
                .block(Duration.ofSeconds(5));

        assertEquals(2, captured.size(), "both clients must have called the door once");
        CapturedRequest blocking = captured.get(0);
        CapturedRequest reactive = captured.get(1);
        assertEquals(blocking.method(), reactive.method(), "HTTP method must match");
        assertEquals(blocking.path(), reactive.path(), "path must match");
        assertEquals(blocking.caller(), reactive.caller(), "caller identity header must match");
        assertEquals(blocking.onBehalfOf(), reactive.onBehalfOf(), "on-behalf-of header must match");
        assertEquals(blocking.contentType(), reactive.contentType(), "content type must match");
        assertEquals(blocking.body(), reactive.body(), "the request body must be byte-identical");
    }

    private static DoorClient blockingClient(DoorConfig config) {
        return new ServiceDoorClient(config.doorUrl(), config.devLoginUser(), config.serviceUser(),
                config.callerHeader(), config.onBehalfOfHeader());
    }

    private static WebClientDoorClient reactiveClient(DoorConfig config) {
        String baseUrl = config.doorUrl().toString();
        DoorProperties properties = new DoorProperties(baseUrl, "/api/govern", "",
                config.serviceUser(), config.callerHeader(), config.onBehalfOfHeader(),
                Duration.ofSeconds(3), Duration.ofSeconds(10));
        WebClient webClient = WebClient.builder().baseUrl(baseUrl).build();
        return new WebClientDoorClient(webClient, properties);
    }
}
