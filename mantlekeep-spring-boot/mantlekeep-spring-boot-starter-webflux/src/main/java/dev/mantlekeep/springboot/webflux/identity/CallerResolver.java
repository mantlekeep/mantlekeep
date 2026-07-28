package dev.mantlekeep.springboot.webflux.identity;

import java.util.Optional;
import org.springframework.web.server.ServerWebExchange;

/**
 * The port that answers "who is making this request?".
 *
 * <p>An adapter decides HOW identity arrives — a header set by the host's SSO gateway, a
 * verified JWT, or mutual TLS — while everything above this port depends only on the fact
 * that a caller was established. Swapping authentication per environment is therefore a
 * configuration change, not a code change.
 *
 * <p>An empty result means "not authenticated". It never means "use a default": defaulting
 * an unknown caller to a name is exactly the hole this port exists to close.
 */
public interface CallerResolver {

    /** The authenticated caller for this exchange, or empty if there is none. */
    Optional<Caller> resolve(ServerWebExchange exchange);
}
