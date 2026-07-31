package dev.mantlekeep.springboot.webflux.identity;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.Optional;
import java.util.Set;
import org.junit.jupiter.api.Test;
import org.springframework.mock.http.server.reactive.MockServerHttpRequest;
import org.springframework.mock.web.server.MockServerWebExchange;
import org.springframework.web.server.ServerWebExchange;

/** The rule under test: identity comes from the gateway, and its absence is not a default. */
class GatewayCallerResolverTest {

    private static final IdentityProperties DEFAULTS =
            new IdentityProperties(null, null, null); // no dev user — production shape

    private static ServerWebExchange request(String... headers) {
        MockServerHttpRequest.BaseBuilder<?> builder = MockServerHttpRequest.get("/api/loop");
        for (int i = 0; i < headers.length; i += 2) {
            builder = builder.header(headers[i], headers[i + 1]);
        }
        return MockServerWebExchange.from(builder.build());
    }

    @Test
    void readsTheCallerAndRolesTheGatewayAsserted() {
        Caller caller = new GatewayCallerResolver(DEFAULTS)
                .resolve(request("X-Mantlekeepkeep-User", "arch-carol",
                        "X-Mantlekeepkeep-Roles", "architect, approver"))
                .orElseThrow();

        assertEquals("arch-carol", caller.name());
        assertEquals(Set.of("architect", "approver"), caller.roles()); // whitespace trimmed
        assertTrue(caller.hasRole("approver"));
    }

    @Test
    void aCallerWithoutRolesIsStillACaller() {
        Caller caller = new GatewayCallerResolver(DEFAULTS)
                .resolve(request("X-Mantlekeepkeep-User", "larry")).orElseThrow();

        assertEquals("larry", caller.name());
        assertTrue(caller.roles().isEmpty()); // authorisation is the door's job, not the header's
    }

    @Test
    void noIdentityHeaderMeansNoCaller_neverADefaultName() {
        assertTrue(new GatewayCallerResolver(DEFAULTS).resolve(request()).isEmpty());
        assertTrue(new GatewayCallerResolver(DEFAULTS)
                .resolve(request("X-Mantlekeepkeep-User", "   ")).isEmpty()); // blank is not a name
    }

    @Test
    void theDevUserAppliesOnlyWhenExplicitlyConfigured() {
        IdentityProperties dev = new IdentityProperties(null, null, "root");

        Optional<Caller> unauthenticated = new GatewayCallerResolver(dev).resolve(request());
        assertEquals("root", unauthenticated.orElseThrow().name());

        // and it never overrides a real asserted identity
        assertEquals("arch-carol", new GatewayCallerResolver(dev)
                .resolve(request("X-Mantlekeepkeep-User", "arch-carol")).orElseThrow().name());
    }

    @Test
    void headerNamesAreConfigurable_becauseGatewaysDiffer() {
        IdentityProperties custom = new IdentityProperties("X-Forwarded-User", "X-Groups", "");
        Caller caller = new GatewayCallerResolver(custom)
                .resolve(request("X-Forwarded-User", "mei", "X-Groups", "devops"))
                .orElseThrow();

        assertEquals("mei", caller.name());
        assertEquals(Set.of("devops"), caller.roles());
    }
}
