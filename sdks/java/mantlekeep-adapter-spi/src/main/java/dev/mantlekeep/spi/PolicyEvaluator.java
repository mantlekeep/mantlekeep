package dev.mantlekeep.spi;

import java.util.Map;

/**
 * The policy port — the STATELESS decision seam of the core (composition-model §4d:
 * "embed the stateless decision; share the stateful chain"). An adapter answers one
 * question: may this subject perform this action, given these attributes?
 *
 * <p>Adapter examples: OPA-as-wasm (sovereign, no network — the air-gap pick),
 * a remote OPA server, or a host-native rules engine. The core knows only this port;
 * which one runs is chosen by config from the ServiceLoader-registered set.
 */
public interface PolicyEvaluator {

    /**
     * Evaluates one governed question. MUST be side-effect free — recording the
     * decision belongs to the chain ({@link StorePort}), never to the evaluator.
     *
     * @param subjectId  who is asking (already authenticated by the host)
     * @param action     the governed action name, e.g. {@code job.promote}
     * @param attributes the intent's parameters (env, scope, resource, …)
     */
    PolicyVerdict evaluate(String subjectId, String action, Map<String, String> attributes);
}
