package dev.mantlekeep.springboot.door;

import reactor.core.publisher.Mono;

/**
 * Submits an {@link Intent} to MantleKeep's one door and returns the {@link Decision}.
 *
 * <p>The reactive contract (WebFlux-first); a blocking starter may adapt it. An
 * implementation governs NOTHING itself — the door decides. On a denial an
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
