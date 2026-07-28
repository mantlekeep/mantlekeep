package dev.mantlekeep.door;

import dev.mantlekeep.door.model.DoorConfig;

/**
 * The ServiceLoader contract a native-core binding registers
 * ({@code META-INF/services/dev.mantlekeep.door.NativeCoreProvider}). Embedded mode
 * selects a binding FROM THIS REGISTERED SET by the configured name
 * ({@code mantlekeep.door.adapters.native-core}) — the same
 * ServiceLoader-never-Class.forName rule as {@link dev.mantlekeep.spi.AdapterProvider};
 * see the security note there.
 */
public interface NativeCoreProvider {

    /** The name config selects this binding by, e.g. {@code "panama"}. */
    String name();

    /** Loads the core per the config (library path, policy documents). */
    NativeCore create(DoorConfig config);
}
