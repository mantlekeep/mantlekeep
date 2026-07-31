package dev.mantlekeep.door.model;

import java.net.URI;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/**
 * Everything the factory needs to build a {@link dev.mantlekeep.door.DoorClient} —
 * the whole "no-code wiring" surface. A product supplies ONLY this (via
 * {@code mantlekeep.door.*} properties in the Spring starter) and writes zero wiring code.
 *
 * @param mode        service (remote door) or embedded (in-process core); default service
 * @param brand       the policy/product namespace ({@code <brand>.rbac}) — the white-label
 *                    seam; default {@code "mantlekeep"}
 * @param doorUrl     the remote door's base URL (service mode)
 * @param corePath    the native core library, e.g. {@code libmantlekeep_core.dylib} (embedded mode)
 * @param policyPaths the policy documents the embedded core loads
 * @param adapters    adapter selection by kind: config key → REGISTERED provider name,
 *                    e.g. {@code store → postgres}, {@code native-core → panama}. Names
 *                    select from the ServiceLoader-registered set only — never class names.
 * @param devLoginUser dev-tier only: a subject to log in as against the remote door's
 *                    {@code /api/login}. Blank in prod — the SSO gateway owns the session.
 */
public record DoorConfig(
        DoorMode mode,
        String brand,
        URI doorUrl,
        Path corePath,
        List<String> policyPaths,
        Map<String, String> adapters,
        String devLoginUser,
        String serviceUser,
        String callerHeader,
        String onBehalfOfHeader) {

    /** The adapters-map key that selects which registered native binding embeds the core. */
    public static final String NATIVE_CORE_ADAPTER_KEY = "native-core";

    public DoorConfig {
        mode = mode == null ? DoorMode.SERVICE : mode;
        brand = brand == null || brand.isBlank() ? "mantlekeep" : brand;
        // WHO THIS APPLICATION IS to the door. Blank means it acts only as itself, and
        // the intent's own subject is sent as the caller instead.
        serviceUser = serviceUser == null ? "" : serviceUser.trim();
        // Header NAMES are configurable because a white-labelled product renames them —
        // a door reading X-Acme-User will not see X-Mantlekeep-User, and the failure is a
        // 401 that looks like a broken service rather than a mismatched setting.
        callerHeader = (callerHeader == null || callerHeader.isBlank())
                ? "X-Mantlekeep-User" : callerHeader;
        onBehalfOfHeader = (onBehalfOfHeader == null || onBehalfOfHeader.isBlank())
                ? "X-Mantlekeep-On-Behalf-Of" : onBehalfOfHeader;
        policyPaths = policyPaths == null ? List.of() : List.copyOf(policyPaths);
        adapters = adapters == null ? Map.of() : Map.copyOf(adapters);
        devLoginUser = devLoginUser == null ? "" : devLoginUser;
    }

    /**
     * Service mode against a remote door, with no service identity of its own — the
     * caller is whatever subject each intent names.
     */
    public static DoorConfig service(URI doorUrl) {
        return new DoorConfig(DoorMode.SERVICE, null, doorUrl, null, null, null, null, null, null, null);
    }

    /**
     * Service mode for an application that authenticates as ITSELF and acts for people.
     * The door records the person as the subject and this application as {@code via}, and
     * will only accept that claim if {@code serviceUser} is on its delegator allowlist.
     */
    public static DoorConfig service(URI doorUrl, String serviceUser) {
        return new DoorConfig(DoorMode.SERVICE, null, doorUrl, null, null, null, null,
                serviceUser, null, null);
    }

    /** Embedded mode with the native core and its policy documents. */
    public static DoorConfig embedded(Path corePath, List<String> policyPaths) {
        return new DoorConfig(DoorMode.EMBEDDED, null, null, corePath, policyPaths, null, null,
                null, null, null);
    }
}
