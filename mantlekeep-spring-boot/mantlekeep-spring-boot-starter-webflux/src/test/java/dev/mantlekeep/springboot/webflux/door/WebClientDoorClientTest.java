package dev.mantlekeep.springboot.webflux.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.springboot.door.Decision;
import dev.mantlekeep.springboot.door.DoorException;
import dev.mantlekeep.springboot.door.DoorProperties;
import dev.mantlekeep.springboot.door.Intent;
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
            new DoorProperties("http://door.local", "/api/govern", "", null, null);

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
                "{\"decision\":\"allow\",\"token\":\"tok-123\",\"policyId\":\"p1\","
                        + "\"expires\":\"2026-07-18T05:35:11Z\"}"));

        StepVerifier.create(client.submit(Intent.of("loop.propose").goal("draft the spec").build()))
                .assertNext(d -> {
                    assertTrue(d.allowed());
                    assertEquals("tok-123", d.token());
                    assertEquals("p1", d.policyId());
                    // the door returns expiry as an RFC-3339 string; it must parse to an Instant
                    assertNotNull(d.expiresAt());
                })
                .verifyComplete();
    }

    @Test
    void denyErrorsWithDoorExceptionCarryingReason() {
        WebClientDoorClient client = clientReturning(json(HttpStatus.FORBIDDEN,
                "{\"decision\":\"deny\",\"reason\":\"no role permits action\"}"));

        StepVerifier.create(client.submit(Intent.of("loop.approve").goal("approve release").build()))
                .expectErrorSatisfies(ex -> {
                    assertInstanceOf(DoorException.class, ex);
                    Decision d = ((DoorException) ex).decision();
                    assertNotNull(d);
                    assertFalse(d.allowed());
                    assertTrue(d.reasons().contains("no role permits action"));
                })
                .verify();
    }

    @Test
    void nonAllowOn2xxStillDenies() {
        // a 200 that is not an allow (e.g. require_approval) must not read as allowed
        WebClientDoorClient client = clientReturning(json(HttpStatus.OK,
                "{\"decision\":\"require_approval\",\"requiredApprovers\":[\"L1-Architect\"]}"));

        StepVerifier.create(client.submit(Intent.of("release.promote").goal("promote to prod").build()))
                .expectError(DoorException.class)
                .verify();
    }

    @Test
    void transportFailureIsWrappedAsDoorException() {
        WebClientDoorClient client = clientReturning(request -> Mono.error(new IOException("connection refused")));

        StepVerifier.create(client.submit(Intent.of("loop.propose").goal("draft").build()))
                .expectError(DoorException.class)
                .verify();
    }
}
