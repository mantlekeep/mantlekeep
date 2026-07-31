package dev.mantlekeep.springboot.webflux.identity;

import dev.mantlekeep.springboot.door.OnBehalfOf;
import java.util.Optional;
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
 */
@Order(Ordered.HIGHEST_PRECEDENCE)
public class CallerWebFilter implements WebFilter {

    private final CallerResolver resolver;

    public CallerWebFilter(CallerResolver resolver) {
        this.resolver = resolver;
    }

    @Override
    public Mono<Void> filter(ServerWebExchange exchange, WebFilterChain chain) {
        if (!exchange.getRequest().getPath().value().startsWith("/api/")) {
            return chain.filter(exchange);
        }
        Optional<Caller> caller = resolver.resolve(exchange);
        if (caller.isEmpty()) {
            exchange.getResponse().setStatusCode(HttpStatus.UNAUTHORIZED);
            return exchange.getResponse().setComplete();
        }
        // The caller travels twice: as the product's own Caller, and as the subject this
        // application asserts to the door. Writing both here is what makes every governed
        // call downstream attributable without a single call site having to remember.
        return chain.filter(exchange)
                .contextWrite(CallerContext.with(caller.get()))
                .contextWrite(OnBehalfOf.with(caller.get().name()));
    }
}
