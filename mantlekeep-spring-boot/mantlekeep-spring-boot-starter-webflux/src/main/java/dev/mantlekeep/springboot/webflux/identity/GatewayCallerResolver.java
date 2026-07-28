package dev.mantlekeep.springboot.webflux.identity;

import java.util.Arrays;
import java.util.LinkedHashSet;
import java.util.Optional;
import java.util.Set;
import java.util.stream.Collectors;
import org.springframework.web.server.ServerWebExchange;

/**
 * Reads the caller from headers set by the gateway that terminated authentication — the
 * host's SSO proxy pattern, where an identity-aware proxy authenticates the user and passes
 * the result downstream.
 *
 * <p><strong>Trust boundary.</strong> These headers are only trustworthy because the app is
 * reachable exclusively through that gateway, which strips any client-supplied copy. Exposing
 * this app directly to a network where a client can set the header would let anyone claim any
 * identity. Deployments that cannot guarantee the gateway must use an adapter that verifies a
 * signed assertion instead — which is why this is one implementation of a port, not the
 * mechanism itself.
 */
public class GatewayCallerResolver implements CallerResolver {

    private final IdentityProperties properties;

    public GatewayCallerResolver(IdentityProperties properties) {
        this.properties = properties;
    }

    @Override
    public Optional<Caller> resolve(ServerWebExchange exchange) {
        String name = exchange.getRequest().getHeaders().getFirst(properties.userHeader());
        if (name == null || name.isBlank()) {
            // No asserted identity. Fall back only to the explicitly configured dev user —
            // blank in every real deployment, so the request is rejected instead.
            return properties.hasDevUser()
                    ? Optional.of(Caller.named(properties.devUser()))
                    : Optional.empty();
        }
        return Optional.of(new Caller(name, rolesFrom(
                exchange.getRequest().getHeaders().getFirst(properties.rolesHeader()))));
    }

    /** Roles arrive comma-separated; blanks are dropped and order is kept for readability. */
    private static Set<String> rolesFrom(String header) {
        if (header == null || header.isBlank()) {
            return Set.of();
        }
        return Arrays.stream(header.split(","))
                .map(String::trim)
                .filter(role -> !role.isEmpty())
                .collect(Collectors.toCollection(LinkedHashSet::new));
    }
}
