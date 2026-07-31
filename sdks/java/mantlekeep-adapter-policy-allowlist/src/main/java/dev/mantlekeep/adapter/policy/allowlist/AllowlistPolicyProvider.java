package dev.mantlekeep.adapter.policy.allowlist;

import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.AdapterProvider;
import dev.mantlekeep.spi.PolicyEvaluator;
import java.util.Map;
import java.util.Set;
import java.util.TreeSet;

/**
 * The ServiceLoader entry point of this adapter jar: registers the allowlist policy
 * under the name {@code "allowlist"} for the {@code policy-evaluator} kind. Declared
 * in {@code META-INF/services/dev.mantlekeep.spi.AdapterProvider}.
 *
 * <p>Config subtree: {@code allowed-actions} — a comma-separated list of governed
 * action names, e.g. {@code allowed-actions: job.build,job.test}. Missing
 * or blank means an EMPTY allowlist (deny everything) — a policy adapter never
 * defaults open.
 *
 * <p><b>SECURITY:</b> an adapter is SELECTED BY NAME from the ServiceLoader-registered
 * set, never loaded via {@code Class.forName(configValue)}. Config picks among what
 * artifacts have registered; it can never name arbitrary code into the governance
 * process.
 */
public final class AllowlistPolicyProvider implements AdapterProvider {

    /** The name config selects this adapter by: {@code mantlekeep.door.adapters.policy-evaluator=allowlist}. */
    public static final String NAME = "allowlist";

    /** The config key holding the comma-separated allowed action names. */
    public static final String ALLOWED_ACTIONS_KEY = "allowed-actions";

    @Override
    public String name() {
        return NAME;
    }

    @Override
    public AdapterKind kind() {
        return AdapterKind.POLICY_EVALUATOR;
    }

    @Override
    public PolicyEvaluator create(Map<String, String> configuration) {
        return new AllowlistPolicyEvaluator(
                parseAllowedActions(configuration.get(ALLOWED_ACTIONS_KEY)));
    }

    private static Set<String> parseAllowedActions(String commaSeparatedActions) {
        Set<String> allowedActions = new TreeSet<>();
        if (commaSeparatedActions == null) {
            return allowedActions;
        }
        for (String actionName : commaSeparatedActions.split(",")) {
            String trimmedActionName = actionName.trim();
            if (!trimmedActionName.isEmpty()) {
                allowedActions.add(trimmedActionName);
            }
        }
        return allowedActions;
    }
}
