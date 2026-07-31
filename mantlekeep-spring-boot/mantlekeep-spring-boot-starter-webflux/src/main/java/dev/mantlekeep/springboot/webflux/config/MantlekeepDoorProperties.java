package dev.mantlekeep.springboot.webflux.config;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Binds {@code mantlekeep.door.*} configuration (constructor binding). Absent values are
 * left {@code null} here and defaulted when mapped to the core
 * {@link dev.mantlekeep.springboot.door.DoorProperties}.
 *
 * <pre>
 * mantle:
 *   door:
 *     base-url: http://localhost:8080
 *     govern-path: /api/govern
 *     bearer-token: ${MANTLEKEEP_DOOR_TOKEN:}
 *     connect-timeout: 3s
 *     response-timeout: 10s
 * </pre>
 */
@ConfigurationProperties(prefix = "mantlekeep.door")
public record MantlekeepDoorProperties(
        String baseUrl,
        String governPath,
        String bearerToken,
        Duration connectTimeout,
        Duration responseTimeout) {
}
