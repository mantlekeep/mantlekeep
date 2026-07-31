package dev.mantlekeep.springboot.webflux.identity;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;

/**
 * Auto-configuration for actor identity: wires the {@link CallerResolver} port to its
 * gateway adapter and installs the fail-closed {@link CallerWebFilter}.
 *
 * <p>Promoted into the SDK from the loop product so that <em>every</em> governed product
 * resolves the actor identically — from the authenticated caller, never a request parameter.
 * That uniformity is the point: separation of duties can only be trusted if no product is free
 * to invent its own, weaker notion of "who is acting". A product picks it up simply by
 * depending on this starter — there is nothing to component-scan and nothing to remember.
 *
 * <p>It lives in the WebFlux starter rather than {@code -core} because identity here is
 * inherently reactive-transport: the filter is a {@link org.springframework.web.server.WebFilter},
 * the resolver reads a {@link org.springframework.web.server.ServerWebExchange}, and the caller
 * travels in the Reactor context. Every bean is {@link ConditionalOnMissingBean}, so a product
 * with a different authentication model (a verified JWT, mutual TLS) overrides a single bean
 * without forking the rest.
 */
@AutoConfiguration
@EnableConfigurationProperties(IdentityProperties.class)
public class MantlekeepIdentityAutoConfiguration {

    private static final Logger log = LoggerFactory.getLogger(MantlekeepIdentityAutoConfiguration.class);

    @Bean
    @ConditionalOnMissingBean
    public CallerResolver callerResolver(IdentityProperties properties) {
        if (properties.hasDevUser()) {
            // Loud on purpose: this setting makes every unauthenticated request act as one
            // named user. Fine on a laptop, a vulnerability anywhere else.
            log.warn("mantlekeep.identity.dev-user is set to '{}' — unauthenticated requests will "
                    + "be treated as this user. Never set this outside local development.",
                    properties.devUser());
        }
        return new GatewayCallerResolver(properties);
    }

    @Bean
    @ConditionalOnMissingBean
    public CallerWebFilter callerWebFilter(CallerResolver resolver, IdentityProperties properties) {
        return new CallerWebFilter(resolver, properties);
    }
}
