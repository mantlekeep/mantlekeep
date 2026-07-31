package dev.mantlekeep.springboot.webflux.identity;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Identity settings ({@code mantlekeep.identity.*}).
 *
 * @param userHeader  header carrying the principal's name, set by the gateway that
 *                    terminated authentication (default {@code X-Mantlekeepkeep-User})
 * @param rolesHeader header carrying comma-separated roles (default {@code X-Mantlekeepkeep-Roles})
 * @param devUser     a local development escape hatch: when non-blank, requests arriving
 *                    with no identity header are treated as this user. <strong>Blank by
 *                    default, and must stay blank anywhere real</strong> — it is a way to
 *                    run the app on a laptop without a gateway, not a fallback. Production
 *                    sets nothing and therefore fails closed.
 */
@ConfigurationProperties(prefix = "mantlekeep.identity")
public record IdentityProperties(String userHeader, String rolesHeader, String devUser) {

    public IdentityProperties {
        userHeader = blankTo(userHeader, "X-Mantlekeepkeep-User");
        rolesHeader = blankTo(rolesHeader, "X-Mantlekeepkeep-Roles");
        devUser = devUser == null ? "" : devUser.trim();
    }

    private static String blankTo(String value, String fallback) {
        return value == null || value.isBlank() ? fallback : value;
    }

    /** True when the local escape hatch is configured — logged loudly at startup. */
    public boolean hasDevUser() {
        return !devUser.isBlank();
    }
}
