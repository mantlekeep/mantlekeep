package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.ExecutionToken;
import dev.mantlekeep.door.model.Intent;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

/**
 * Drives the REAL wire path against a JDK {@code com.sun.net.httpserver} stub of the
 * door — the same {@code /api/govern} + {@code /api/audit} contract the portal serves.
 * No external HTTP library on either side.
 */
class ServiceDoorClientTest {

    private HttpServer doorStub;
    private ServiceDoorClient doorClient;
    private volatile String lastGovernRequestBody;

    @BeforeEach
    void startDoorStub() throws IOException {
        doorStub = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        doorStub.createContext("/api/govern", exchange -> {
            lastGovernRequestBody =
                    new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            String action = JsonText.stringField(lastGovernRequestBody, "action");
            String response = "service.deploy".equals(action)
                    ? "{\"decision\":\"deny\",\"reason\":\"segregation of duties: the proposer cannot approve\"}"
                    : "{\"decision\":\"allow\",\"token\":\"WIRE-TOKEN-1\",\"expires\":\"2026-07-26T00:00:00Z\"}";
            respond(exchange, response);
        });
        doorStub.createContext("/api/audit", exchange -> respond(exchange,
                "{\"intact\":true,\"count\":2,\"records\":["
                        + "{\"ts\":\"2026-07-26T10:00:00Z\",\"action\":\"job.build\",\"subject\":\"lead-bob\",\"decision\":\"allow\",\"hash\":\"h2\",\"prevHash\":\"h1\"},"
                        + "{\"ts\":\"2026-07-26T09:00:00Z\",\"action\":\"service.deploy\",\"subject\":\"user-amy\",\"decision\":\"deny\",\"hash\":\"h1\",\"prevHash\":\"h0\"}"
                        + "]}"));
        doorStub.start();
        doorClient = new ServiceDoorClient(
                URI.create("http://127.0.0.1:" + doorStub.getAddress().getPort()), "");
    }

    @AfterEach
    void stopDoorStub() {
        doorClient.close();
        doorStub.stop(0);
    }

    @Test
    void allowCarriesTheWireTokenAndTheEnvParameterIsLifted() {
        Intent promote = new Intent("", null, "job.promote", "project/demo",
                "ship 1.2", Map.of("env", "PROD"));

        ExecutionToken token = doorClient.submit(promote);

        assertEquals("WIRE-TOKEN-1", token.value());
        assertTrue(lastGovernRequestBody.contains("\"env\":\"PROD\""),
                "env must ride the wire's top-level field");
        assertTrue(lastGovernRequestBody.contains("\"goal\":\"ship 1.2\""));
    }

    @Test
    void denyThrowsWithTheDoorsReason() {
        DoorDeniedException denial = assertThrows(DoorDeniedException.class,
                () -> doorClient.submit(Intent.of("service.deploy", "deploy the service")));
        assertTrue(denial.reason().contains("segregation of duties"));
    }

    @Test
    void auditParsesTheWireRecordsAndVerifyReadsIntact() {
        List<AuditRecord> chain = doorClient.audit();
        assertEquals(2, chain.size());
        assertEquals("job.build", chain.get(0).action());
        assertEquals("lead-bob", chain.get(0).subjectId());
        assertEquals("deny", chain.get(1).decision());
        assertTrue(doorClient.verify());
    }

    private static void respond(com.sun.net.httpserver.HttpExchange exchange, String bodyJson)
            throws IOException {
        byte[] body = bodyJson.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", "application/json");
        exchange.sendResponseHeaders(200, body.length);
        try (OutputStream out = exchange.getResponseBody()) {
            out.write(body);
        }
    }
}
