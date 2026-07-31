package dev.mantlekeep.springboot.webflux.config;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Binds {@code mantlekeep.door.*} configuration (constructor binding). Absent values are
 * left {@code null} here and defaulted when mapped to the core
 * {@link dev.mantlekeep.springboot.door.DoorProperties}.
 *
 * <pre>
 * mantlekeep:
 *   door:
 *     base-url: http://localhost:8080
 *     govern-path: /api/govern
 *     bearer-token: ${MANTLEKEEP_DOOR_TOKEN:}
 *     service-user: session-service      # who THIS service is to the door
 *     service-user-header: X-Mantlekeep-User
 *     on-behalf-of-header: X-Mantlekeep-On-Behalf-Of   # rename WITH the caller header
 *     connect-timeout: 3s
 *     response-timeout: 10s
 * </pre>
 *
 * <p>{@code service-user} is required whenever this application acts for a person: the door
 * authenticates the application before it will accept the application's claim about
 * someone else, and the name must be on the door's delegator allowlist.
 */
@ConfigurationProperties(prefix = "mantlekeep.door")
public record MantlekeepDoorProperties(
        String baseUrl,
        String governPath,
        String bearerToken,
        String serviceUser,
        String serviceUserHeader,
        String onBehalfOfHeader,
        Duration connectTimeout,
        Duration responseTimeout) {
}
