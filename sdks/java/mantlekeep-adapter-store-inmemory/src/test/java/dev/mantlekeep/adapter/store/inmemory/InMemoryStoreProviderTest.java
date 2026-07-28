package dev.mantlekeep.adapter.store.inmemory;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.AdapterCatalog;
import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.StorePort;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/**
 * Proves the extension recipe END TO END with the real machinery: this jar's
 * {@code META-INF/services} registration is DISCOVERED by ServiceLoader (via
 * {@link AdapterCatalog#discover()}, exactly as production wiring does) and the
 * adapter is then SELECTED by its config name — while a name outside the registered
 * set fails fast, listing the menu.
 */
class InMemoryStoreProviderTest {

    private final AdapterCatalog catalog = AdapterCatalog.discover();

    @Test
    void serviceLoaderDiscoversThisAdapterJar() {
        assertEquals(List.of(InMemoryStoreProvider.NAME),
                catalog.registeredNames(AdapterKind.STORE));
    }

    @Test
    void configNameSelectsAWorkingStoreAdapter() {
        Object adapter = catalog.select(AdapterKind.STORE, "inmemory", Map.of());

        StorePort store = assertInstanceOf(StorePort.class, adapter);
        store.append("{\"action\":\"job.build\",\"decision\":\"allow\"}");
        store.append("{\"action\":\"job.promote\",\"decision\":\"deny\"}");
        assertEquals(2, store.readAll().size());
        assertTrue(store.readAll().get(0).contains("job.build"));
    }

    @Test
    void anUnknownNameFailsFastListingTheRegisteredMenu() {
        // The attack this design closes: config naming arbitrary code. The name is
        // rejected against the registered set — it is never treated as a class name.
        IllegalArgumentException failure = assertThrows(IllegalArgumentException.class,
                () -> catalog.select(AdapterKind.STORE, "com.evil.Backdoor", Map.of()));
        assertTrue(failure.getMessage().contains("com.evil.Backdoor"));
        assertTrue(failure.getMessage().contains("inmemory"));
        assertTrue(failure.getMessage().contains("never loaded by class name"));
    }
}
