package dev.mantlekeep.springboot.webflux.intent;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.springboot.door.DoorClient;
import dev.mantlekeep.springboot.door.DoorException;
import dev.mantlekeep.springboot.intent.MantlekeepIntent;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;
import org.springframework.aop.aspectj.annotation.AspectJProxyFactory;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

class MantlekeepIntentAspectTest {

    /** A target whose governed body flips {@code ran} only when it actually executes. */
    static class GovernedService {
        final AtomicBoolean ran;

        GovernedService(AtomicBoolean ran) {
            this.ran = ran;
        }

        @MantlekeepIntent(value = "loop.propose", goal = "draft the spec")
        public Mono<String> propose() {
            return Mono.fromSupplier(() -> {
                ran.set(true);
                return "drafted";
            });
        }
    }

    private GovernedService proxied(DoorClient door, AtomicBoolean ran) {
        AspectJProxyFactory factory = new AspectJProxyFactory(new GovernedService(ran));
        factory.addAspect(new MantlekeepIntentAspect(door));
        return factory.getProxy();
    }

    private static Decision allow() {
        // SDK Decision order: (outcome, token, policyId, reasons, requiredApprovers, expiresAt)
        return new Decision(Decision.Outcome.ALLOW, "tok", "p1", null, null, null);
    }

    @Test
    void allowRunsTheBody() {
        AtomicBoolean ran = new AtomicBoolean(false);
        GovernedService svc = proxied(intent -> Mono.just(allow()), ran);

        StepVerifier.create(svc.propose()).expectNext("drafted").verifyComplete();
        assertTrue(ran.get());
    }

    @Test
    void denyBlocksTheBody() {
        AtomicBoolean ran = new AtomicBoolean(false);
        Decision deny = new Decision(Decision.Outcome.DENY, "", "p1",
                List.of(new Decision.Reason("DENY_ACTION_NOT_ALLOWED", "nope")), null, null);
        GovernedService svc = proxied(intent -> Mono.error(new DoorException("denied", deny)), ran);

        StepVerifier.create(svc.propose()).expectError(DoorException.class).verify();
        assertFalse(ran.get(), "the body must NOT run when the door denies");
    }

    @Test
    void submitsIntentBuiltFromAnnotation() {
        AtomicReference<Intent> captured = new AtomicReference<>();
        GovernedService svc = proxied(intent -> {
            captured.set(intent);
            return Mono.just(allow());
        }, new AtomicBoolean(false));

        svc.propose().block();
        assertEquals("loop.propose", captured.get().action());
        assertEquals("draft the spec", captured.get().goal());
    }
}
