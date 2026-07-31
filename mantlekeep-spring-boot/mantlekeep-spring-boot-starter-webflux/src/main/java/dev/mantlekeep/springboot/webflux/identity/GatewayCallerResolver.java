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
        if (!isWellFormedName(name)) {
            // Refuse rather than clean up. This name becomes the SUBJECT on an audit
            // record — a permanent entry in a tamper-evident ledger — so accepting a
            // malformed one and sanitising it later would mean the record no longer says
            // what the caller asserted. A name that cannot be recorded faithfully is not
            // a name this boundary should accept.
            return Optional.empty();
        }
        return Optional.of(new Caller(name, rolesFrom(
                exchange.getRequest().getHeaders().getFirst(properties.rolesHeader()))));
    }

    /** A subject name long enough for any real directory, short enough to bound a record. */
    private static final int MAX_NAME_LENGTH = 128;

    /**
     * A caller-asserted name is only well formed if it can be written faithfully into an
     * audit record and read back: no control characters, and bounded in length.
     *
     * <p>Control characters are refused because this value reaches logs and the chain. A
     * name containing a newline could forge a log line, and would sit in the ledger as
     * something an auditor cannot read as a single identity.
     */
    private static boolean isWellFormedName(String name) {
        if (name.length() > MAX_NAME_LENGTH) {
            return false;
        }
        for (int i = 0; i < name.length(); i++) {
            if (Character.isISOControl(name.charAt(i))) {
                return false;
            }
        }
        return true;
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
