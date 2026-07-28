package dev.mantlekeep.adapter.store.inmemory;

import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.AdapterProvider;
import dev.mantlekeep.spi.StorePort;
import java.util.Map;

/**
 * The ServiceLoader entry point of this adapter jar: registers the in-memory store
 * under the name {@code "inmemory"} for the {@code store} kind. Declared in
 * {@code META-INF/services/dev.mantlekeep.spi.AdapterProvider} — putting this jar on the
 * classpath is what makes {@code store: inmemory} a legal config value.
 *
 * <p><b>SECURITY:</b> an adapter is SELECTED BY NAME from the ServiceLoader-registered
 * set, never loaded via {@code Class.forName(configValue)}. Config picks among what
 * artifacts have registered; it can never name arbitrary code into the governance
 * process.
 */
public final class InMemoryStoreProvider implements AdapterProvider {

    /** The name config selects this adapter by: {@code mantlekeep.door.adapters.store=inmemory}. */
    public static final String NAME = "inmemory";

    @Override
    public String name() {
        return NAME;
    }

    @Override
    public AdapterKind kind() {
        return AdapterKind.STORE;
    }

    /** This adapter needs no configuration — the map is accepted and ignored. */
    @Override
    public StorePort create(Map<String, String> configuration) {
        return new InMemoryStore();
    }
}
