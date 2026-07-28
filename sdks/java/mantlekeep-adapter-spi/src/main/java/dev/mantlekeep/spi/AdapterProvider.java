package dev.mantlekeep.spi;

import java.util.Map;

/**
 * The ServiceLoader contract every adapter jar implements — the ONLY way an adapter
 * enters the SDK. An adapter jar registers its provider in
 * {@code META-INF/services/dev.mantlekeep.spi.AdapterProvider}; config then SELECTS a
 * provider from that registered set by {@link #name()}.
 *
 * <p><b>SECURITY — why ServiceLoader and never {@code Class.forName(configValue)}:</b>
 * in a governance product, loading a class named by a free-form config string is a
 * config-injection hole — whoever writes config gets arbitrary code execution inside
 * the very process that grants approvals. ServiceLoader inverts that: only classes an
 * artifact on the classpath has REGISTERED (typed, declared in META-INF/services) are
 * discoverable, and config can merely pick among them by name. Config chooses policy;
 * it can never reach past the registered set — the sealed-floor rule applied to wiring.
 * It is also air-gap and native-image safe: discovery is static metadata, no reflection
 * over arbitrary names.
 */
public interface AdapterProvider {

    /** The name config selects this adapter by, e.g. {@code "opa-wasm"}, {@code "postgres"}. */
    String name();

    /** Which port this adapter satisfies. */
    AdapterKind kind();

    /**
     * Creates the adapter instance. The returned object MUST implement
     * {@link AdapterKind#portType() kind().portType()} — the SDK verifies this at
     * wiring time and rejects the provider otherwise.
     *
     * @param configuration the adapter's own config subtree (flat key→value)
     */
    Object create(Map<String, String> configuration);
}
