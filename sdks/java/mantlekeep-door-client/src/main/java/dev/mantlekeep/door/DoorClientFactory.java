package dev.mantlekeep.door;

import dev.mantlekeep.door.model.DoorConfig;
import dev.mantlekeep.spi.AdapterKind;
import java.util.List;
import java.util.Map;
import java.util.ServiceLoader;

/**
 * Builds the one {@link DoorClient} a product needs, from {@link DoorConfig} alone —
 * the whole point of the config-driven SDK: the product writes ZERO wiring code.
 *
 * <ul>
 *   <li>{@code mode=service} → {@link ServiceDoorClient} against {@code doorUrl}</li>
 *   <li>{@code mode=embedded} → {@link EmbeddedDoorClient} over a {@link NativeCore}
 *       binding selected from the ServiceLoader-REGISTERED set (drop
 *       {@code mantlekeep-java-core} on the runtime classpath; select it by name via
 *       {@code adapters.native-core}, or let a sole registered binding win)</li>
 * </ul>
 *
 * <p><b>SECURITY:</b> every selection here is name-into-a-registered-set
 * (ServiceLoader), NEVER {@code Class.forName(configValue)} — see
 * {@link AdapterCatalog} for why that rule exists. Unknown names fail fast at
 * startup, listing what IS registered.
 */
public final class DoorClientFactory {

    private DoorClientFactory() {
    }

    /** Builds the client for this config; validates adapter selections up front. */
    public static DoorClient create(DoorConfig config) {
        failFastOnUnknownAdapterSelections(config);
        return switch (config.mode()) {
            case SERVICE -> createServiceClient(config);
            case EMBEDDED -> createEmbeddedClient(config);
        };
    }

    private static DoorClient createServiceClient(DoorConfig config) {
        if (config.doorUrl() == null) {
            throw new IllegalArgumentException(
                    "mantlekeep.door.mode=service requires mantlekeep.door.url (the remote door's base URL)");
        }
        // Pass the CONFIGURED header names through. Dropping them here would silently
        // fall back to the framework's own names, so a white-labelled deployment would
        // send X-Mantlekeep-User to a door reading X-Acme-User — a 401 that looks like a
        // broken service rather than a setting that never arrived.
        return new ServiceDoorClient(config.doorUrl(), config.devLoginUser(),
                config.serviceUser(), config.callerHeader(), config.onBehalfOfHeader());
    }

    private static DoorClient createEmbeddedClient(DoorConfig config) {
        List<NativeCoreProvider> registeredBindings =
                ServiceLoader.load(NativeCoreProvider.class).stream()
                        .map(ServiceLoader.Provider::get)
                        .toList();
        if (registeredBindings.isEmpty()) {
            throw new IllegalStateException(
                    "mantlekeep.door.mode=embedded but no NativeCoreProvider is registered — "
                            + "add mantlekeep-java-core (the Panama binding) to the runtime classpath");
        }
        String selectedName = config.adapters().get(DoorConfig.NATIVE_CORE_ADAPTER_KEY);
        NativeCoreProvider binding = selectBinding(registeredBindings, selectedName);
        return new EmbeddedDoorClient(binding.create(config));
    }

    private static NativeCoreProvider selectBinding(
            List<NativeCoreProvider> registeredBindings, String selectedName) {
        if (selectedName == null || selectedName.isBlank()) {
            if (registeredBindings.size() == 1) {
                return registeredBindings.get(0);
            }
            throw new IllegalStateException(
                    "several native-core bindings are registered " + names(registeredBindings)
                            + " — choose one via mantlekeep.door.adapters.native-core");
        }
        return registeredBindings.stream()
                .filter(candidate -> candidate.name().equals(selectedName))
                .findFirst()
                .orElseThrow(() -> new IllegalArgumentException(
                        "no registered native-core binding named '" + selectedName
                                + "' — registered: " + names(registeredBindings)));
    }

    /**
     * Every adapter entry in config must name a REGISTERED provider — a typo is a
     * startup failure with the menu printed, never a mid-request surprise.
     * ({@code native-core} has its own registry, checked in embedded mode.)
     */
    private static void failFastOnUnknownAdapterSelections(DoorConfig config) {
        AdapterCatalog catalog = AdapterCatalog.discover();
        for (Map.Entry<String, String> selection : config.adapters().entrySet()) {
            if (DoorConfig.NATIVE_CORE_ADAPTER_KEY.equals(selection.getKey())) {
                continue;
            }
            AdapterKind kind = AdapterKind.fromConfigKey(selection.getKey());
            if (!catalog.registeredNames(kind).contains(selection.getValue())) {
                throw new IllegalArgumentException(
                        "config selects " + kind.configKey() + " adapter '" + selection.getValue()
                                + "' but only these are registered: " + catalog.registeredNames(kind));
            }
        }
    }

    private static List<String> names(List<NativeCoreProvider> bindings) {
        return bindings.stream().map(NativeCoreProvider::name).sorted().toList();
    }
}
