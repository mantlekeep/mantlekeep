package dev.mantlekeep.door;

/**
 * The port over the embedded core's FFI surface (composition-model §4c: a handful
 * of calls — submit, audit, verify — kept ruthlessly small and stable). This module
 * knows ONLY this port; the actual Panama/FFM binding lives in {@code mantlekeep-java-core},
 * an optional runtime drop-in — so this module compiles and tests with no native
 * library anywhere near it.
 */
public interface NativeCore extends AutoCloseable {

    /** Submits one intent (JSON, the core's versioned contract) → the result JSON. */
    String submitJson(String intentJson);

    /** The full audit chain as a JSON array. */
    String auditJson();

    /** Walks the chain inside the core; {@code true} = intact. */
    boolean verifyChain();

    /** Frees the core handle. */
    @Override
    void close();
}
