package dev.mantlekeep.spi;

/**
 * The kinds of adapter the SDK can select by config, each pinned to its port type.
 * The pinning is what makes config-driven selection TYPED: a selected adapter is
 * checked against {@link #portType()} at wiring time, so a provider that lies about
 * its kind fails fast instead of surfacing as a ClassCastException mid-request.
 */
public enum AdapterKind {

    POLICY_EVALUATOR("policy-evaluator", PolicyEvaluator.class),
    STORE("store", StorePort.class),
    WORKER("worker", WorkerPort.class),
    AGENT("agent", AgentPort.class);

    private final String configKey;
    private final Class<?> portType;

    AdapterKind(String configKey, Class<?> portType) {
        this.configKey = configKey;
        this.portType = portType;
    }

    /** The key products use in config, e.g. {@code mantlekeep.door.adapters.policy-evaluator}. */
    public String configKey() {
        return configKey;
    }

    /** The port interface every adapter of this kind must implement. */
    public Class<?> portType() {
        return portType;
    }

    /** Resolves a config key ({@code "store"}, {@code "policy-evaluator"}) to its kind. */
    public static AdapterKind fromConfigKey(String configKey) {
        for (AdapterKind kind : values()) {
            if (kind.configKey.equals(configKey)) {
                return kind;
            }
        }
        throw new IllegalArgumentException(
                "unknown adapter kind '" + configKey + "' — known kinds: "
                        + String.join(", ", knownConfigKeys()));
    }

    private static String[] knownConfigKeys() {
        AdapterKind[] kinds = values();
        String[] keys = new String[kinds.length];
        for (int index = 0; index < kinds.length; index++) {
            keys[index] = kinds[index].configKey;
        }
        return keys;
    }
}
