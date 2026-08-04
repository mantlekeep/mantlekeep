package dev.mantlekeep.springboot.saga;

import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * In-memory {@link SagaTimeline} — the MVP: it proves the shape and is lost on restart. The
 * durable, chain-correlated adapter (StorePort-backed, anchored to the chain head) is the follow-up
 * the operation ledger has too. Registered as a bean so any product on the starter gets one unless
 * it supplies its own.
 *
 * <p>Concurrency-safe because steps are recorded from the worker's off-thread execution.
 */
public class InMemorySagaTimeline implements SagaTimeline {

    private final ConcurrentHashMap<String, CopyOnWriteArrayList<SagaStep>> byOperation =
            new ConcurrentHashMap<>();

    @Override
    public void record(SagaStep step) {
        byOperation.computeIfAbsent(step.operationId(), id -> new CopyOnWriteArrayList<>()).add(step);
    }

    @Override
    public List<SagaStep> forOperation(String operationId) {
        return List.copyOf(byOperation.getOrDefault(operationId, new CopyOnWriteArrayList<>()));
    }
}
