package dev.mantlekeep.springboot.webflux.intent;

import dev.mantlekeep.springboot.door.Decision;
import dev.mantlekeep.springboot.door.DoorClient;
import dev.mantlekeep.springboot.door.Intent;
import dev.mantlekeep.springboot.intent.MantlekeepIntent;
import org.aspectj.lang.ProceedingJoinPoint;
import org.aspectj.lang.annotation.Around;
import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.reflect.MethodSignature;
import reactor.core.publisher.Flux;
import reactor.core.publisher.Mono;

/**
 * Governs {@link MantlekeepIntent}-annotated methods: it submits the intent to the door
 * FIRST, and the method's reactive body runs only if the door allows.
 *
 * <p>Because {@link DoorClient#submit} errors on a denial, the composition is simply
 * "submit THEN proceed": on allow the gate emits and the body runs; on deny the gate
 * errors and the body never subscribes — its side effects never happen.
 *
 * <p>WebFlux starter contract: an annotated method MUST return {@code Mono} or
 * {@code Flux}. (The mvc starter will handle blocking methods.)
 */
@Aspect
public class MantlekeepIntentAspect {

    private final DoorClient doorClient;

    public MantlekeepIntentAspect(DoorClient doorClient) {
        this.doorClient = doorClient;
    }

    @Around("@annotation(mantleIntent)")
    public Object govern(ProceedingJoinPoint pjp, MantlekeepIntent mantleIntent) {
        Intent intent = toIntent(mantleIntent);
        Mono<Decision> gate = doorClient.submit(intent);

        Class<?> returnType = ((MethodSignature) pjp.getSignature()).getReturnType();
        if (Flux.class.isAssignableFrom(returnType)) {
            return gate.thenMany(Flux.defer(() -> proceedFlux(pjp)));
        }
        return gate.then(Mono.defer(() -> proceedMono(pjp)));
    }

    private static Intent toIntent(MantlekeepIntent annotation) {
        String action = annotation.value();
        String goal = annotation.goal().isBlank() ? "invoke " + action : annotation.goal();
        return Intent.of(action).resource(annotation.resource()).goal(goal).build();
    }

    @SuppressWarnings("unchecked")
    private static Mono<Object> proceedMono(ProceedingJoinPoint pjp) {
        try {
            return (Mono<Object>) pjp.proceed();
        } catch (Throwable t) {
            return Mono.error(t);
        }
    }

    @SuppressWarnings("unchecked")
    private static Flux<Object> proceedFlux(ProceedingJoinPoint pjp) {
        try {
            return (Flux<Object>) pjp.proceed();
        } catch (Throwable t) {
            return Flux.error(t);
        }
    }
}
