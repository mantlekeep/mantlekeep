package dev.mantlekeep.spring;

import dev.mantlekeep.door.DoorClient;
import dev.mantlekeep.door.model.Intent;
import java.util.Map;
import org.aspectj.lang.ProceedingJoinPoint;
import org.aspectj.lang.annotation.Around;
import org.aspectj.lang.annotation.Aspect;

/**
 * Govern-BEFORE-execute, made structural: every {@code @MantlekeepIntent} method is
 * intercepted, its intent submitted through the one door, and only an allow lets
 * the body run. A deny throws {@link dev.mantlekeep.door.DoorDeniedException} — the
 * effect after the door simply never happens. (The Spring-Security-filter-chain
 * analogy: the door is the filter, the method is the protected resource.)
 *
 * <p>Rides Spring AOP — the runtime's own, safe, auditable interception — never
 * hand-rolled reflection.
 */
@Aspect
public final class MantlekeepIntentAspect {

    private final DoorClient doorClient;
    private final SubjectResolver subjectResolver;

    public MantlekeepIntentAspect(DoorClient doorClient, SubjectResolver subjectResolver) {
        this.doorClient = doorClient;
        this.subjectResolver = subjectResolver;
    }

    @Around("@annotation(governedIntent)")
    public Object governBeforeExecute(ProceedingJoinPoint joinPoint, MantlekeepIntent governedIntent)
            throws Throwable {
        String goal = governedIntent.goal().isBlank()
                ? joinPoint.getSignature().toShortString()
                : governedIntent.goal();
        Intent intent = new Intent(
                "",
                subjectResolver.currentSubject(),
                governedIntent.action(),
                governedIntent.resource(),
                goal,
                Map.of());

        doorClient.submit(intent); // deny → DoorDeniedException; the body never runs

        return joinPoint.proceed();
    }
}
