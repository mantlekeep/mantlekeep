package dev.mantlekeep.springboot.door;

import java.time.Duration;

/**
 * Settings for reaching the door. A plain value object with safe defaults — the starter
 * binds it from configuration ({@code mantlekeep.door.*}) so the core stays framework-free.
 *
 * @param baseUrl         the door's base URL (default {@code http://localhost:8080})
 * @param governPath      the govern endpoint path (default {@code /api/govern})
 * @param bearerToken     the bearer token the SDK presents; the gateway/auth owns identity
 * @param serviceUser     WHO THIS SERVICE IS to the door — the service-account name it
 *                        authenticates as. The door records it as {@code via} when the
 *                        service acts for a person, and will only accept that claim if the
 *                        name is on its delegator allowlist. Blank means this service never
 *                        acts for anyone else.
 * @param serviceUserHeader the header carrying {@code serviceUser} (default
 *                        {@code X-Mantlekeep-User} — the same header the door reads as the
 *                        authenticated caller)
 * @param onBehalfOfHeader the header naming the person this service acts for (default
 *                        {@code X-Mantlekeep-On-Behalf-Of}). It travels WITH
 *                        {@code serviceUserHeader} and must be renamed with it: a door
 *                        reading a branded pair will see one header it recognises and one
 *                        it does not, so the caller authenticates and the delegation is
 *                        silently dropped — the action is then recorded against the
 *                        SERVICE rather than the person.
 * @param connectTimeout  connection timeout (default 3s)
 * @param responseTimeout response timeout (default 10s)
 */
public record DoorProperties(
        String baseUrl,
        String governPath,
        String bearerToken,
        String serviceUser,
        String serviceUserHeader,
        String onBehalfOfHeader,
        Duration connectTimeout,
        Duration responseTimeout) {

    public DoorProperties {
        baseUrl = (baseUrl == null || baseUrl.isBlank()) ? "http://localhost:8080" : baseUrl;
        governPath = (governPath == null || governPath.isBlank()) ? "/api/govern" : governPath;
        bearerToken = bearerToken == null ? "" : bearerToken;
        serviceUser = serviceUser == null ? "" : serviceUser.trim();
        serviceUserHeader = (serviceUserHeader == null || serviceUserHeader.isBlank())
                ? "X-Mantlekeep-User" : serviceUserHeader;
        onBehalfOfHeader = (onBehalfOfHeader == null || onBehalfOfHeader.isBlank())
                ? "X-Mantlekeep-On-Behalf-Of" : onBehalfOfHeader;
        connectTimeout = connectTimeout == null ? Duration.ofSeconds(3) : connectTimeout;
        responseTimeout = responseTimeout == null ? Duration.ofSeconds(10) : responseTimeout;
    }
}
