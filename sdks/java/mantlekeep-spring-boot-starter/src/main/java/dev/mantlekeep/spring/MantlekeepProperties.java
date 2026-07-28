package dev.mantlekeep.spring;

import dev.mantlekeep.door.model.DoorConfig;
import dev.mantlekeep.door.model.DoorMode;
import java.net.URI;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * The whole product-facing configuration surface — {@code mantlekeep.door.*}. Spring
 * binds it (constructor binding on a record); the compact constructor supplies the
 * defaults so a minimal config is a valid config.
 *
 * <pre>{@code
 * mantlekeep:
 *   door:
 *     mode: service            # or: embedded
 *     url: http://mantlekeep-door:8080
 *     brand: mantlekeep            # the <brand>.rbac policy namespace (white-label seam)
 *     adapters:
 *       native-core: panama    # names select from the ServiceLoader-REGISTERED set
 * }</pre>
 *
 * @param mode         service | embedded (the one config flip; default service)
 * @param brand        policy namespace; default "mantlekeep"
 * @param url          the remote door (service mode)
 * @param corePath     the native core library file (embedded mode)
 * @param policyPaths  policy documents for the embedded core
 * @param adapters     adapter kind → REGISTERED provider name
 * @param subject      the fallback subject id when no SubjectResolver bean overrides it
 * @param devLoginUser dev tier only — door login user; leave blank behind the SSO gateway
 */
@ConfigurationProperties(prefix = "mantlekeep.door")
public record MantlekeepProperties(
        DoorMode mode,
        String brand,
        URI url,
        Path corePath,
        List<String> policyPaths,
        Map<String, String> adapters,
        String subject,
        String devLoginUser) {

    public MantlekeepProperties {
        mode = mode == null ? DoorMode.SERVICE : mode;
        brand = brand == null || brand.isBlank() ? "mantlekeep" : brand;
        policyPaths = policyPaths == null ? List.of() : List.copyOf(policyPaths);
        adapters = adapters == null ? Map.of() : Map.copyOf(adapters);
        subject = subject == null ? "" : subject;
        devLoginUser = devLoginUser == null ? "" : devLoginUser;
    }

    /** The framework-agnostic config the factory consumes. */
    public DoorConfig toDoorConfig() {
        return new DoorConfig(mode, brand, url, corePath, policyPaths, adapters, devLoginUser);
    }
}
