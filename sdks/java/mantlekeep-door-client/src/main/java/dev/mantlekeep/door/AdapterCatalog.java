package dev.mantlekeep.door;

import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.AdapterProvider;
import java.util.List;
import java.util.Map;
import java.util.ServiceLoader;
import java.util.stream.Collectors;

/**
 * The REGISTERED set of adapters — everything a {@code META-INF/services} entry on
 * the classpath has declared, discovered once via {@link ServiceLoader}. Config
 * selects FROM this set by name; nothing outside it is reachable.
 *
 * <p><b>SECURITY:</b> this catalog is the reason the SDK never calls
 * {@code Class.forName(configValue)}. A free-form class name in config would let
 * whoever writes config execute arbitrary code inside the governance process.
 * Here, config can only pick among providers an artifact has REGISTERED — typed,
 * discoverable, air-gap and native-image safe. An unknown name fails fast at
 * startup with the registered names listed, not at request time.
 */
public final class AdapterCatalog {

    private final List<AdapterProvider> registeredProviders;

    /** Tests hand the providers in explicitly; production uses {@link #discover()}. */
    public AdapterCatalog(List<AdapterProvider> registeredProviders) {
        this.registeredProviders = List.copyOf(registeredProviders);
    }

    /** Discovers every provider registered on the classpath. */
    public static AdapterCatalog discover() {
        return new AdapterCatalog(
                ServiceLoader.load(AdapterProvider.class).stream()
                        .map(ServiceLoader.Provider::get)
                        .toList());
    }

    /**
     * Creates the adapter a config entry selected, verifying the instance really
     * implements the kind's port type.
     *
     * @throws IllegalArgumentException when no provider of that kind has that name
     * @throws IllegalStateException    when the provider returns the wrong type
     */
    public Object select(AdapterKind kind, String providerName, Map<String, String> configuration) {
        AdapterProvider provider = registeredProviders.stream()
                .filter(candidate -> candidate.kind() == kind)
                .filter(candidate -> candidate.name().equals(providerName))
                .findFirst()
                .orElseThrow(() -> new IllegalArgumentException(
                        "no registered " + kind.configKey() + " adapter named '" + providerName
                                + "' — registered: " + registeredNames(kind)
                                + " (adapters are ServiceLoader-registered, never loaded by class name)"));
        Object adapter = provider.create(configuration);
        if (!kind.portType().isInstance(adapter)) {
            throw new IllegalStateException(
                    "adapter provider '" + providerName + "' returned " + describe(adapter)
                            + " which does not implement " + kind.portType().getName());
        }
        return adapter;
    }

    /** The names registered for one kind — the full menu config may choose from. */
    public List<String> registeredNames(AdapterKind kind) {
        return registeredProviders.stream()
                .filter(candidate -> candidate.kind() == kind)
                .map(AdapterProvider::name)
                .sorted()
                .collect(Collectors.toList());
    }

    private static String describe(Object adapter) {
        return adapter == null ? "null" : adapter.getClass().getName();
    }
}
