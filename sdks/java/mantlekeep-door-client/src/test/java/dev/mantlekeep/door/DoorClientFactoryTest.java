package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.model.DoorConfig;
import dev.mantlekeep.door.model.DoorMode;
import dev.mantlekeep.door.model.Intent;
import java.net.URI;
import java.util.Map;
import org.junit.jupiter.api.Test;

/**
 * Proves the factory's config-driven selection: mode picks the implementation, and
 * the embedded binding comes from the ServiceLoader-REGISTERED set (the test
 * registers {@code in-memory} via test META-INF/services) — never from a class name.
 */
class DoorClientFactoryTest {

    @Test
    void serviceModeBuildsTheWireClient() {
        DoorConfig config = DoorConfig.service(URI.create("http://localhost:8080"));
        try (DoorClient doorClient = DoorClientFactory.create(config)) {
            assertInstanceOf(ServiceDoorClient.class, doorClient);
        }
    }

    @Test
    void serviceModeWithoutUrlFailsFast() {
        DoorConfig config = new DoorConfig(DoorMode.SERVICE, null, null, null, null, null, null);
        IllegalArgumentException failure =
                assertThrows(IllegalArgumentException.class, () -> DoorClientFactory.create(config));
        assertTrue(failure.getMessage().contains("mantlekeep.door.url"));
    }

    @Test
    void embeddedModeSelectsTheRegisteredBindingByName() {
        DoorConfig config = new DoorConfig(DoorMode.EMBEDDED, null, null, null, null,
                Map.of(DoorConfig.NATIVE_CORE_ADAPTER_KEY, "in-memory"), null);
        try (DoorClient doorClient = DoorClientFactory.create(config)) {
            assertInstanceOf(EmbeddedDoorClient.class, doorClient);
            assertTrue(doorClient.decide(Intent.of("job.build", "prove wiring")).allowed());
            assertEquals(1, doorClient.audit().size());
        }
    }

    @Test
    void embeddedModeWithSingleRegisteredBindingNeedsNoName() {
        DoorConfig config = DoorConfig.embedded(null, null);
        try (DoorClient doorClient = DoorClientFactory.create(config)) {
            assertInstanceOf(EmbeddedDoorClient.class, doorClient);
        }
    }

    @Test
    void unknownBindingNameFailsFastListingTheRegisteredSet() {
        DoorConfig config = new DoorConfig(DoorMode.EMBEDDED, null, null, null, null,
                Map.of(DoorConfig.NATIVE_CORE_ADAPTER_KEY, "com.evil.Backdoor"), null);
        IllegalArgumentException failure =
                assertThrows(IllegalArgumentException.class, () -> DoorClientFactory.create(config));
        assertTrue(failure.getMessage().contains("in-memory"),
                "the failure must list what IS registered");
    }

    @Test
    void unknownAdapterSelectionFailsFastAtStartup() {
        DoorConfig config = new DoorConfig(DoorMode.SERVICE, null,
                URI.create("http://localhost:8080"), null, null,
                Map.of("store", "not-registered"), null);
        IllegalArgumentException failure =
                assertThrows(IllegalArgumentException.class, () -> DoorClientFactory.create(config));
        assertTrue(failure.getMessage().contains("store"));
    }
}
