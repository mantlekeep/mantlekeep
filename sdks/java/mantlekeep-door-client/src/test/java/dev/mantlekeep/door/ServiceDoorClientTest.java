package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
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
 * door — the canonical {@code /api/govern} response: an outcome, the policy that decided,
 * typed reasons, the approvers a require_approval needs, and when an allow expires.
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
            respond(exchange, switch (action) {
                case "service.deploy" -> "{\"outcome\":\"deny\",\"policyId\":\"policy.sdlc.v3\","
                        + "\"reasons\":[{\"code\":\"DENY_SEPARATION_OF_DUTIES\","
                        + "\"message\":\"segregation of duties: the proposer cannot approve\"}]}";
                case "release.cut" -> "{\"outcome\":\"require_approval\",\"policyId\":\"policy.sdlc.v3\","
                        + "\"requiredApprovers\":[\"L4-Approver\",\"security-officer\"],"
                        + "\"reasons\":[{\"code\":\"REQUIRE_APPROVAL\","
                        + "\"message\":\"a second approver must sign off\"}]}";
                default -> "{\"outcome\":\"allow\",\"token\":\"WIRE-TOKEN-1\","
                        + "\"policyId\":\"policy.sdlc.v3\",\"expiresAt\":\"2026-07-26T00:00:00Z\","
                        + "\"reasons\":[]}";
            });
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
    void allowDecodesThePolicyAndExpiryTheAuditorAsksFor() {
        Decision decision = doorClient.decide(
                new Intent("", null, "job.promote", "project/demo", "ship 1.2", Map.of()));

        assertEquals(Decision.Outcome.ALLOW, decision.outcome());
        assertTrue(decision.allowed());
        assertEquals("policy.sdlc.v3", decision.policyId());
        assertEquals("2026-07-26T00:00:00Z", decision.expiresAt());
    }

    @Test
    void denyThrowsWithTheDoorsReason() {
        DoorDeniedException denial = assertThrows(DoorDeniedException.class,
                () -> doorClient.submit(Intent.of("service.deploy", "deploy the service")));
        assertTrue(denial.reason().contains("segregation of duties"));
    }

    @Test
    void denyCarriesTheTypedCode() {
        Decision decision = doorClient.decide(Intent.of("service.deploy", "deploy the service"));

        assertEquals(Decision.Outcome.DENY, decision.outcome());
        assertFalse(decision.allowed());
        assertEquals(1, decision.reasons().size());
        assertEquals("DENY_SEPARATION_OF_DUTIES", decision.reasons().get(0).code());
        assertTrue(decision.reasons().get(0).message().contains("segregation of duties"));
    }

    @Test
    void requireApprovalNamesWhoMustSignOff() {
        Decision decision = doorClient.decide(Intent.of("release.cut", "cut the 1.2 release"));

        assertEquals(Decision.Outcome.REQUIRE_APPROVAL, decision.outcome());
        assertFalse(decision.allowed());
        assertEquals(List.of("L4-Approver", "security-officer"), decision.requiredApprovers());
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
