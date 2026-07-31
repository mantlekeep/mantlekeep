package dev.mantlekeep.core;

import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.NativeCoreProvider;
import dev.mantlekeep.door.model.DoorConfig;
import dev.mantlekeep.door.model.DoorMode;
import java.util.ServiceLoader;
import org.junit.jupiter.api.Test;

/**
 * Proves the ServiceLoader REGISTRATION (a typo in META-INF/services fails here) and
 * the fail-fast config validation — WITHOUT loading the native library. Tests that
 * exercise the real dylib live in the spike's parity harness, not in unit scope.
 */
class PanamaMantlekeepCoreProviderTest {

    @Test
    void thePanamaBindingIsDiscoverableByServiceLoader() {
        boolean panamaRegistered = ServiceLoader.load(NativeCoreProvider.class).stream()
                .map(ServiceLoader.Provider::get)
                .anyMatch(provider -> "panama".equals(provider.name()));
        assertTrue(panamaRegistered,
                "META-INF/services must register the panama binding for config to select it");
    }

    @Test
    void aMissingCorePathFailsFastNamingTheProperty() {
        DoorConfig configWithoutCorePath =
                new DoorConfig(DoorMode.EMBEDDED, null, null, null, null, null, null, null, null, null);
        IllegalArgumentException failure = assertThrows(IllegalArgumentException.class,
                () -> new PanamaMantlekeepCoreProvider().create(configWithoutCorePath));
        assertTrue(failure.getMessage().contains("core-path"));
    }
}
