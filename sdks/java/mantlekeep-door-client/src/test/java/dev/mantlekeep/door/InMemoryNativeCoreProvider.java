package dev.mantlekeep.door;

import dev.mantlekeep.door.model.DoorConfig;
import java.util.List;

/**
 * Registered in this module's TEST {@code META-INF/services} so the factory's
 * ServiceLoader selection path is proven end-to-end without any native library.
 */
public final class InMemoryNativeCoreProvider implements NativeCoreProvider {

    @Override
    public String name() {
        return "in-memory";
    }

    @Override
    public NativeCore create(DoorConfig config) {
        return new InMemoryNativeCore(List.of("service.deploy"));
    }
}
