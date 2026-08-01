package dev.mantlekeep.springboot.webflux.door;

import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.springboot.door.DoorClient;
import dev.mantlekeep.springboot.door.DoorException;
import dev.mantlekeep.springboot.door.DoorProperties;
import dev.mantlekeep.springboot.door.OnBehalfOf;
import java.util.List;
import java.util.Map;
import org.springframework.web.reactive.function.client.WebClient;
import reactor.core.publisher.Mono;

/**
 * Reactive {@link DoorClient} over Spring {@link WebClient}: POSTs the intent to the
 * door's govern endpoint and maps the canonical response to a {@link Decision}.
 *
 * <p>It carries the pure-JDK spine's {@link Intent} and {@link Decision} value types — the
 * ONE definition — and adds only the {@code Mono} transport. On a governance denial (or any
 * non-2xx status) the returned {@code Mono} errors with a {@link DoorException} carrying the
 * decision; a transport failure errors with a {@code DoorException} wrapping the cause. It
 * governs nothing itself — the door decides.
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

    /**
     * The DEFAULT header naming the subject this application acts for. It is a default,
     * not a constant: a branded deployment renames it together with the caller header,
     * and renaming only one is worse than renaming neither — the door recognises the
     * caller, does not recognise the delegation, and records the action against the
     * SERVICE instead of the person, with nothing reporting that it happened.
     */
    static final String DEFAULT_ON_BEHALF_OF = "X-Mantlekeep-On-Behalf-Of";

    @Override
    public Mono<Decision> submit(Intent intent) {
        return OnBehalfOf.current().flatMap(subject -> submit(intent, subject));
    }

    private Mono<Decision> submit(Intent intent, String onBehalfOf) {
        // env is read generically off the intent's parameters — the spine names no
        // environment; it rides the wire's top-level `env` field the Go door reads.
        Object environmentValue = intent.parameters().get("env");
        String environment = environmentValue == null ? "" : environmentValue.toString();
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
                        headers.set(properties.onBehalfOfHeader(), onBehalfOf);
                    }
                })
                .bodyValue(new GovernRequest(
                        intent.action(), intent.resource(), intent.goal(), environment, intent.parameters()))
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
        return new Decision(mapOutcome(success, body), body.token(), body.policyId(),
                reasonsOf(body), body.requiredApprovers(), body.expiresAt());
    }

    private static Decision.Outcome mapOutcome(boolean success, DoorResponse body) {
        String outcome = body.outcome() == null ? "" : body.outcome().trim().toLowerCase();
        return switch (outcome) {
            case "allow" -> Decision.Outcome.ALLOW;
            case "deny" -> Decision.Outcome.DENY;
            case "require_approval" -> Decision.Outcome.REQUIRE_APPROVAL;
            default -> success ? Decision.Outcome.ALLOW : Decision.Outcome.DENY; // fall back on the status
        };
    }

    /**
     * Maps the canonical typed reasons ({@code [{code,message}]}) to the spine's
     * {@link Decision.Reason}. Falls back leniently to a single untyped reason from an
     * older {@code reason}/{@code error} field, so a pre-canonical or error payload still
     * yields a readable denial rather than an empty one.
     */
    private static List<Decision.Reason> reasonsOf(DoorResponse body) {
        if (body.reasons() != null && !body.reasons().isEmpty()) {
            return body.reasons().stream()
                    .map(reason -> new Decision.Reason(reason.code(), reason.message()))
                    .toList();
        }
        String single = body.reason() != null ? body.reason() : body.error();
        return single == null ? List.of() : List.of(new Decision.Reason("", single));
    }

    /**
     * The outbound body, matching the door's frozen wire contract exactly:
     * {@code {action, resource, goal, env, params}}.
     *
     * <p>It is built explicitly rather than serialising the {@link Intent} record, because
     * the record also carries {@code id} and {@code subject} the door reads from headers,
     * not the body — sending them is a silent divergence from the canonical shape. Identity
     * never travels in the body; it is set as a header, because a body-supplied caller would
     * be forgeable.
     */
    private record GovernRequest(
            String action, String resource, String goal, String env, Map<String, ?> params) {
    }
}
