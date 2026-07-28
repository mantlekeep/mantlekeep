package dev.mantlekeep.core;

import dev.mantlekeep.door.NativeCore;
import dev.mantlekeep.door.NativeCoreProvider;
import dev.mantlekeep.door.model.DoorConfig;

/**
 * Registers the Panama binding under the name {@code "panama"} — dropping this jar
 * on the runtime classpath is ALL it takes for {@code mantlekeep.door.mode=embedded}
 * to find it (ServiceLoader; config selects by name, never by class name).
 */
public final class PanamaMantlekeepCoreProvider implements NativeCoreProvider {

    @Override
    public String name() {
        return "panama";
    }

    @Override
    public NativeCore create(DoorConfig config) {
        if (config.corePath() == null) {
            throw new IllegalArgumentException(
                    "the panama binding needs mantlekeep.door.core-path "
                            + "(the native core library, e.g. libmantlekeep_core.dylib)");
        }
        return new PanamaMantlekeepCore(config.corePath(), config.policyPaths());
    }
}
