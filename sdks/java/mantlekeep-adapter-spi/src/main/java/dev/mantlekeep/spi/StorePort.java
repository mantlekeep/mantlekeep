package dev.mantlekeep.spi;

import java.util.List;

/**
 * The audit-chain port — the STATEFUL half of the core (composition-model §4d).
 * One logical chain, many appenders: an embedded host appends here; the adapter
 * decides where the chain lives (embedded file store for a sovereign air-gap zone,
 * a shared Postgres/log store for scaled pods — never one authoritative chain per pod).
 *
 * <p>Records cross this port as JSON text: the chain's record shape is owned by the
 * core's versioned contract, not by any adapter, and JSON keeps this SPI dependency-free.
 */
public interface StorePort {

    /** Appends one audit record (JSON) to the chain. Append-only — there is no update. */
    void append(String auditRecordJson);

    /** Reads the full chain, oldest first, each entry as the JSON it was appended with. */
    List<String> readAll();
}
