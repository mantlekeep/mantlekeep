package dev.mantlekeep.adapter.store.inmemory;

import dev.mantlekeep.spi.StorePort;
import java.util.ArrayList;
import java.util.List;

/**
 * A {@link StorePort} that keeps the audit chain in process memory — the simplest
 * possible chain appender. It exists to teach the adapter PATTERN (and to back tests
 * and dev runs); a real zone picks a durable adapter (file, Postgres) by config.
 *
 * <p>Append-only by construction: there is no update or delete path, matching the
 * chain's contract. Thread-safe — a door under load appends from many threads.
 */
public final class InMemoryStore implements StorePort {

    private final List<String> auditRecords = new ArrayList<>();

    @Override
    public synchronized void append(String auditRecordJson) {
        auditRecords.add(auditRecordJson);
    }

    @Override
    public synchronized List<String> readAll() {
        return List.copyOf(auditRecords);
    }
}
