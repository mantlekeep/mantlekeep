package dev.mantlekeep.springboot.webflux.identity;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import java.util.Optional;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.mock.http.server.reactive.MockServerHttpRequest;
import org.springframework.mock.web.server.MockServerWebExchange;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

/** The filter is the boundary: no identity, no API. */
class CallerWebFilterTest {

    private static MockServerWebExchange exchange(String path, String user) {
        MockServerHttpRequest.BaseBuilder<?> builder = MockServerHttpRequest.get(path);
        if (user != null) {
            builder = builder.header("X-Mantlekeep-User", user);
        }
        return MockServerWebExchange.from(builder.build());
    }

    private static CallerWebFilter filter(String devUser) {
        return new CallerWebFilter(
                new GatewayCallerResolver(new IdentityProperties(null, null, devUser)));
    }

    @Test
    void anUnauthenticatedApiRequestIsRejectedAndNeverReachesTheController() {
        MockServerWebExchange exchange = exchange("/api/loop/LOOP-1/approve", null);

        StepVerifier.create(filter("").filter(exchange,
                        ex -> Mono.error(new AssertionError("the chain must not be reached")))) 
                .verifyComplete();

        assertEquals(HttpStatus.UNAUTHORIZED, exchange.getResponse().getStatusCode());
    }

    @Test
    void anAuthenticatedRequestCarriesTheCallerDownstream() {
        MockServerWebExchange exchange = exchange("/api/loop/LOOP-1/approve", "arch-carol");

        Mono<Void> chain = filter("").filter(exchange,
                ex -> CallerContext.current()
                        .doOnNext(caller -> assertEquals("arch-carol", caller.name()))
                        .then());

        StepVerifier.create(chain).verifyComplete();
        assertNull(exchange.getResponse().getStatusCode()); // not rejected
    }

    @Test
    void staticResourcesAreExempt_theApiIsNot() {
        MockServerWebExchange asset = exchange("/workspace-mock.html", null);

        StepVerifier.create(filter("").filter(asset, ex -> Mono.empty())).verifyComplete();
        assertNull(asset.getResponse().getStatusCode()); // served without identity
    }

    @Test
    void readingTheCallerWithoutTheFilterFailsLoudlyRatherThanGuessing() {
        StepVerifier.create(CallerContext.current())
                .verifyErrorMessage(
                        "no authenticated caller in context — CallerWebFilter did not run");
    }

    @Test
    void theDevUserLetsTheAppRunWithNoGatewayInFront() {
        MockServerWebExchange exchange = exchange("/api/loop", null);

        Mono<Void> chain = filter("root").filter(exchange,
                ex -> CallerContext.current()
                        .doOnNext(caller -> assertEquals("root", caller.name()))
                        .then());

        StepVerifier.create(chain).verifyComplete();
        assertEquals(Optional.empty(), Optional.ofNullable(exchange.getResponse().getStatusCode()));
    }
}
