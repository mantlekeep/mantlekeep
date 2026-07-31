package dev.mantlekeep.springboot.webflux.identity;

import dev.mantlekeep.springboot.door.OnBehalfOf;
import java.util.Optional;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ServerWebExchange;
import org.springframework.web.server.WebFilter;
import org.springframework.web.server.WebFilterChain;
import reactor.core.publisher.Mono;

/**
 * Establishes the caller for every API request, or rejects it.
 *
 * <p>Fails closed: an API request with no resolvable identity is answered {@code 401} and
 * never reaches a controller. That is the point — while identity was optional, an anonymous
 * request could name itself the approver.
 *
 * <p>Static resources are exempt so the portal can load its own login-less assets; nothing
 * under {@code /api} is exempt.
 *
 * <h2>Why this logs, when the chain records everything</h2>
 *
 * <p>The chain does not record everything: it records DECISIONS, and a request refused here
 * never reaches the door, so it produces no record at all. Without this log an attempt
 * carrying no identity is invisible — nothing in the ledger, nothing anywhere. A refusal is
 * therefore logged at WARN, because it is the only evidence that it happened.
 *
 * <p>An accepted request logs at DEBUG rather than INFO: what it goes on to do IS on the
 * chain, and logging it again at INFO buries the refusals — the entries worth reading — in
 * ordinary traffic.
 *
 * <p>The refusal distinguishes two cases, because they mean different things: the header was
 * <em>absent</em> (usually a misconfigured client, or a probe) versus <em>present but
 * unresolvable</em> (someone naming an identity the directory does not know, which is the
 * more interesting of the two).
 */
@Order(Ordered.HIGHEST_PRECEDENCE)
public class CallerWebFilter implements WebFilter {

    private static final Logger log = LoggerFactory.getLogger(CallerWebFilter.class);

    /** Caps a caller-supplied value in a log line: long enough to identify, short enough not to flood. */
    private static final int MAX_LOGGED_VALUE = 64;

    private final CallerResolver resolver;
    private final IdentityProperties identity;

    public CallerWebFilter(CallerResolver resolver, IdentityProperties identity) {
        this.resolver = resolver;
        this.identity = identity;
    }

    @Override
    public Mono<Void> filter(ServerWebExchange exchange, WebFilterChain chain) {
        if (!exchange.getRequest().getPath().value().startsWith("/api/")) {
            return chain.filter(exchange);
        }
        Optional<Caller> caller = resolver.resolve(exchange);
        if (caller.isEmpty()) {
            logRefusal(exchange);
            exchange.getResponse().setStatusCode(HttpStatus.UNAUTHORIZED);
            return exchange.getResponse().setComplete();
        }
        if (log.isDebugEnabled()) {
            log.debug("caller {} on {} {}", safe(caller.get().name()),
                    exchange.getRequest().getMethod(), safe(exchange.getRequest().getPath().value()));
        }
        // The caller travels twice: as the product's own Caller, and as the subject this
        // application asserts to the door. Writing both here is what makes every governed
        // call downstream attributable without a single call site having to remember.
        return chain.filter(exchange)
                .contextWrite(CallerContext.with(caller.get()))
                .contextWrite(OnBehalfOf.with(caller.get().name()));
    }

    private void logRefusal(ServerWebExchange exchange) {
        String asserted = exchange.getRequest().getHeaders().getFirst(identity.userHeader());
        String method = String.valueOf(exchange.getRequest().getMethod());
        String path = safe(exchange.getRequest().getPath().value());
        String from = remoteAddress(exchange);

        if (asserted == null || asserted.isBlank()) {
            log.warn("refused {} {} from {}: no {} header — the caller sent no identity",
                    method, path, from, identity.userHeader());
        } else {
            log.warn("refused {} {} from {}: {}={} did not resolve to a known subject",
                    method, path, from, identity.userHeader(), safe(asserted));
        }
    }

    private static String remoteAddress(ServerWebExchange exchange) {
        return exchange.getRequest().getRemoteAddress() == null
                ? "unknown"
                : String.valueOf(exchange.getRequest().getRemoteAddress().getAddress());
    }

    /**
     * Makes a caller-supplied value safe to write to a log.
     *
     * <p>Everything on the refusal path came from the request, so it is controlled by
     * whoever is being refused. Written raw, a value containing newlines would let them
     * inject entire log lines — forging entries in the record that exists to hold them to
     * account, which matters more here than in an ordinary application. Control characters
     * become dots and the value is truncated.
     */
    private static String safe(String value) {
        if (value == null) {
            return "";
        }
        StringBuilder cleaned = new StringBuilder(Math.min(value.length(), MAX_LOGGED_VALUE));
        for (int i = 0; i < value.length() && cleaned.length() < MAX_LOGGED_VALUE; i++) {
            char c = value.charAt(i);
            cleaned.append(Character.isISOControl(c) ? '.' : c);
        }
        return value.length() > MAX_LOGGED_VALUE ? cleaned + "…" : cleaned.toString();
    }
}
