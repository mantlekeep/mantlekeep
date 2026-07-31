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
        String devLoginUser) {

    /** The adapters-map key that selects which registered native binding embeds the core. */
    public static final String NATIVE_CORE_ADAPTER_KEY = "native-core";

    public DoorConfig {
        mode = mode == null ? DoorMode.SERVICE : mode;
        brand = brand == null || brand.isBlank() ? "mantlekeep" : brand;
        policyPaths = policyPaths == null ? List.of() : List.copyOf(policyPaths);
        adapters = adapters == null ? Map.of() : Map.copyOf(adapters);
        devLoginUser = devLoginUser == null ? "" : devLoginUser;
    }

    /** Service mode against a remote door — the production shape. */
    public static DoorConfig service(URI doorUrl) {
        return new DoorConfig(DoorMode.SERVICE, null, doorUrl, null, null, null, null);
    }

    /** Embedded mode with the native core and its policy documents. */
    public static DoorConfig embedded(Path corePath, List<String> policyPaths) {
        return new DoorConfig(DoorMode.EMBEDDED, null, null, corePath, policyPaths, null, null);
    }
}
