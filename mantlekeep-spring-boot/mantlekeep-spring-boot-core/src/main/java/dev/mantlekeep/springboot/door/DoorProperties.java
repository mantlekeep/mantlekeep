package dev.mantlekeep.springboot.door;

import java.time.Duration;

/**
 * Settings for reaching the door. A plain value object with safe defaults — the starter
 * binds it from configuration ({@code mantlekeep.door.*}) so the core stays framework-free.
 *
 * @param baseUrl         the door's base URL (default {@code http://localhost:8080})
 * @param governPath      the govern endpoint path (default {@code /api/govern})
 * @param bearerToken     the bearer token the SDK presents; the gateway/auth owns identity
 * @param connectTimeout  connection timeout (default 3s)
 * @param responseTimeout response timeout (default 10s)
 */
public record DoorProperties(
        String baseUrl,
        String governPath,
        String bearerToken,
        Duration connectTimeout,
        Duration responseTimeout) {

    public DoorProperties {
        baseUrl = (baseUrl == null || baseUrl.isBlank()) ? "http://localhost:8080" : baseUrl;
        governPath = (governPath == null || governPath.isBlank()) ? "/api/govern" : governPath;
        bearerToken = bearerToken == null ? "" : bearerToken;
        connectTimeout = connectTimeout == null ? Duration.ofSeconds(3) : connectTimeout;
        responseTimeout = responseTimeout == null ? Duration.ofSeconds(10) : responseTimeout;
    }
}
