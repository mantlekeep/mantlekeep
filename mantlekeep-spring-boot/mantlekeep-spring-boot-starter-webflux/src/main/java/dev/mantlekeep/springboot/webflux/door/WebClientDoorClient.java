package dev.mantlekeep.springboot.webflux.door;

import dev.mantlekeep.springboot.door.Decision;
import dev.mantlekeep.springboot.door.DoorClient;
import dev.mantlekeep.springboot.door.DoorException;
import dev.mantlekeep.springboot.door.DoorProperties;
import dev.mantlekeep.springboot.door.Intent;
import dev.mantlekeep.springboot.door.OnBehalfOf;
import java.time.Instant;
import java.util.List;
import org.springframework.web.reactive.function.client.WebClient;
import reactor.core.publisher.Mono;

/**
 * Reactive {@link DoorClient} over Spring {@link WebClient}: POSTs the intent to the
 * door's govern endpoint and maps the response to a {@link Decision}.
 *
 * <p>On a governance denial (or any non-2xx status) the returned {@code Mono} errors
 * with a {@link DoorException} carrying the decision; a transport failure errors with a
 * {@code DoorException} wrapping the cause. It governs nothing itself — the door decides.
 *
 * <p>When the caller established an {@link OnBehalfOf} subject, it travels as a header so the
 * chain records who ACTED rather than merely which application called. It is read from the
 * reactive context here rather than threaded through {@code submit} so that no product call
 * site can forget it — the attribution is either present for every call or absent for all.
 */
public class WebClientDoorClient implements DoorClient {

    private final WebClient webClient;
    private final DoorProperties properties;

    public WebClientDoorClient(WebClient webClient, DoorProperties properties) {
        this.webClient = webClient;
        this.properties = properties;
    }

    /** Header naming the subject this application acts for; the door decides if it may. */
    static final String ON_BEHALF_OF = "X-Mantlekeep-On-Behalf-Of";

    @Override
    public Mono<Decision> submit(Intent intent) {
        return OnBehalfOf.current().flatMap(subject -> submit(intent, subject));
    }

    private Mono<Decision> submit(Intent intent, String onBehalfOf) {
        return webClient.post()
                .uri(properties.governPath())
                .headers(headers -> {
                    // WHO THIS SERVICE IS. The door authenticates the application first;
                    // only then will it accept the application's claim about a person.
                    // Without this the door sees an unidentified caller and refuses.
                    if (!properties.serviceUser().isBlank()) {
                        headers.set(properties.serviceUserHeader(), properties.serviceUser());
                    }
                    // WHO THE SERVICE ACTS FOR. Recorded as the subject, with the service
                    // as `via` — the door decides whether this service may make the claim.
                    if (!onBehalfOf.isBlank()) {
                        headers.set(ON_BEHALF_OF, onBehalfOf);
                    }
                })
                .bodyValue(intent)
                .exchangeToMono(response -> response.bodyToMono(DoorResponse.class)
                        .defaultIfEmpty(DoorResponse.empty())
                        .map(body -> toDecision(response.statusCode().is2xxSuccessful(), body)))
                .flatMap(decision -> decision.allowed()
                        ? Mono.just(decision)
                        : Mono.error(new DoorException(
                                "door denied intent '" + intent.action() + "'", decision)))
                .onErrorMap(ex -> ex instanceof DoorException ? ex
                        : new DoorException("door call failed for '" + intent.action() + "'", ex));
    }

    private Decision toDecision(boolean success, DoorResponse body) {
        return new Decision(mapOutcome(success, body), body.token(), parseExpiry(body.expires()),
                body.policyId(), reasonsOf(body), body.requiredApprovers());
    }

    /** The door returns expiry as an RFC-3339 instant string (e.g. 2026-07-18T05:35:11Z). */
    private static Instant parseExpiry(String expires) {
        if (expires == null || expires.isBlank()) {
            return null;
        }
        try {
            return Instant.parse(expires);
        } catch (java.time.format.DateTimeParseException ex) {
            return null;
        }
    }

    private static Decision.Outcome mapOutcome(boolean success, DoorResponse body) {
        String decision = body.decision() == null ? "" : body.decision().trim().toLowerCase();
        return switch (decision) {
            case "allow" -> Decision.Outcome.ALLOW;
            case "deny" -> Decision.Outcome.DENY;
            case "require_approval" -> Decision.Outcome.REQUIRE_APPROVAL;
            default -> success ? Decision.Outcome.ALLOW : Decision.Outcome.DENY; // fall back on the status
        };
    }

    private static List<String> reasonsOf(DoorResponse body) {
        if (body.reasons() != null && !body.reasons().isEmpty()) {
            return body.reasons();
        }
        String single = body.reason() != null ? body.reason() : body.error();
        return single == null ? List.of() : List.of(single);
    }
}
