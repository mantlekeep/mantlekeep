package dev.mantlekeep.springboot.webflux.error;

import dev.mantlekeep.springboot.door.DoorException;
import java.util.Map;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

/**
 * Maps a {@link DoorException} to a clean HTTP response for EVERY product on the starter, instead of
 * a 500. A governance DENIAL ({@code decision() != null}) is an EXPECTED outcome — the order was
 * refused by the floor/policy — so it becomes a client-facing {@code 403} carrying the door's
 * reasons. A transport-level failure reaching the door ({@code decision() == null}) is a {@code 503}
 * — nothing was decided, so it is safe to retry.
 *
 * <p>Registered by {@code MantlekeepAutoConfiguration} as a {@code @ConditionalOnMissingBean}, so a
 * product may override it (define its own {@code DoorExceptionHandler} bean or advice). Reactive-safe:
 * an exception signalled in the {@code Mono}/{@code Flux} chain of a WebFlux handler reaches
 * {@code @ExceptionHandler} the same as a synchronously thrown one.
 */
@RestControllerAdvice
public class DoorExceptionHandler {

    @ExceptionHandler(DoorException.class)
    public ResponseEntity<Map<String, Object>> onDoor(DoorException e) {
        String detail = e.getMessage() == null ? "door call failed" : e.getMessage();
        if (e.decision() != null) {
            // A governance denial — the floor/policy refused this order. 403, with the reasons.
            return ResponseEntity.status(HttpStatus.FORBIDDEN).body(Map.of(
                    "status", "denied",
                    "reasons", e.decision().reasons(),
                    "detail", detail));
        }
        // The door could not be reached (transport). 503 — nothing was decided; retry.
        return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE).body(Map.of(
                "status", "unavailable",
                "detail", detail));
    }
}
