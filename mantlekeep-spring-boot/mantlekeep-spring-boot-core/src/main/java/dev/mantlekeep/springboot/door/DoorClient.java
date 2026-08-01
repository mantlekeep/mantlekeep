package dev.mantlekeep.springboot.door;

import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import reactor.core.publisher.Mono;

/**
 * Submits an {@link Intent} to MantleKeep's one door and returns the {@link Decision}.
 *
 * <p>This is the REACTIVE adapter over the pure-JDK door spine ({@code dev.mantlekeep.door}).
 * It reuses the spine's {@code Intent} and {@code Decision} value types — there is ONE
 * definition of each — and adds only what WebFlux needs: a {@code Mono} return. It is not a
 * second definition of what an intent or a verdict is; it is {@code Mono} vocabulary wrapped
 * around the one that already exists.
 *
 * <p>An implementation governs NOTHING itself — the door decides. On a denial an
 * implementation errors the {@code Mono} with a {@link DoorException} carrying the
 * decision, so callers can react without inspecting a status code.
 */
public interface DoorClient {

    /**
     * Submit an intent for governance.
     *
     * @param intent the action to authorize (never {@code null})
     * @return a {@code Mono} emitting the allow decision, or erroring with a
     *         {@link DoorException} on denial or transport failure
     */
    Mono<Decision> submit(Intent intent);
}
