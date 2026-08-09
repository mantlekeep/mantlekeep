package dev.mantlekeep.springboot.webflux.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.springboot.door.DoorException;
import dev.mantlekeep.springboot.door.DoorProperties;
import java.time.Duration;
import java.io.IOException;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.web.reactive.function.client.ClientResponse;
import org.springframework.web.reactive.function.client.ExchangeFunction;
import org.springframework.web.reactive.function.client.WebClient;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

class WebClientDoorClientTest {

    private static final DoorProperties PROPS =
            new DoorProperties("http://door.local", "/api/govern", "", "session-service", null, null, null, null);

    private WebClientDoorClient clientReturning(ExchangeFunction exchange) {
        WebClient webClient = WebClient.builder().baseUrl(PROPS.baseUrl()).exchangeFunction(exchange).build();
        return new WebClientDoorClient(webClient, PROPS);
    }

    private static ExchangeFunction json(HttpStatus status, String body) {
        return request -> Mono.just(ClientResponse.create(status)
                .header("Content-Type", MediaType.APPLICATION_JSON_VALUE)
                .body(body)
                .build());
    }

    @Test
    void allowEmitsDecisionWithToken() {
        WebClientDoorClient client = clientReturning(json(HttpStatus.OK,
                "{\"outcome\":\"allow\",\"token\":\"tok-123\",\"policyId\":\"p1\","
                        + "\"expiresAt\":\"2026-07-18T05:35:11Z\",\"reasons\":[]}"));

        StepVerifier.create(client.submit(Intent.of("loop.propose", "draft the spec")))
                .assertNext(decision -> {
                    assertTrue(decision.allowed());
                    assertEquals("tok-123", decision.token());
                    assertEquals("p1", decision.policyId());
                    // the door returns expiry as an RFC-3339 STRING on the canonical wire
                    assertEquals("2026-07-18T05:35:11Z", decision.expiresAt());
                })
                .verifyComplete();
    }

    @Test
    void denyErrorsWithDoorExceptionCarryingTypedReason() {
        WebClientDoorClient client = clientReturning(json(HttpStatus.FORBIDDEN,
                "{\"outcome\":\"deny\",\"policyId\":\"p1\",\"reasons\":[{\"code\":"
                        + "\"DENY_ACTION_NOT_ALLOWED\",\"message\":\"no role permits action\"}]}"));

        StepVerifier.create(client.submit(Intent.of("loop.approve", "approve release")))
                .expectErrorSatisfies(ex -> {
                    assertInstanceOf(DoorException.class, ex);
                    Decision decision = ((DoorException) ex).decision();
                    assertFalse(decision.allowed());
                    assertEquals("DENY_ACTION_NOT_ALLOWED", decision.reasons().get(0).code());
                    assertTrue(decision.reasons().get(0).message().contains("no role permits action"));
                })
                .verify();
    }

    @Test
    void nonAllowOn2xxStillDenies() {
        // a 200 that is not an allow (require_approval) must not read as allowed
        WebClientDoorClient client = clientReturning(json(HttpStatus.OK,
                "{\"outcome\":\"require_approval\",\"requiredApprovers\":[\"L1-Architect\"]}"));

        StepVerifier.create(client.submit(Intent.of("release.promote", "promote to prod")))
                .expectError(DoorException.class)
                .verify();
    }

    @Test
    void transportFailureIsWrappedAsDoorException() {
        WebClientDoorClient client = clientReturning(request -> Mono.error(new IOException("connection refused")));

        StepVerifier.create(client.submit(Intent.of("loop.propose", "draft")))
                .expectError(DoorException.class)
                .verify();
    }

    @Test
    void aHungDoorFailsClosedWithinTheResponseTimeout() {
        // 0.1.1: submit() now APPLIES its responseTimeout. A hung door (never answers) must fail CLOSED —
        // a DoorException within the timeout — not hang. Prove-fail-first: without .timeout() in submit(),
        // Mono.never() never completes and this verify() times out.
        DoorProperties fast = new DoorProperties(
                "http://door.local", "/api/govern", "", "session-service", null, null, null, Duration.ofMillis(50));
        WebClient webClient = WebClient.builder().baseUrl(fast.baseUrl())
                .exchangeFunction(request -> Mono.never()).build();
        WebClientDoorClient client = new WebClientDoorClient(webClient, fast);

        StepVerifier.create(client.submit(Intent.of("session.deploy", "deploy a session")))
                .expectError(DoorException.class)
                .verify(Duration.ofSeconds(2));
    }
}
