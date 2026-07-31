package dev.mantlekeep.adapter.policy.allowlist;

import dev.mantlekeep.spi.PolicyEvaluator;
import dev.mantlekeep.spi.PolicyVerdict;
import java.util.Map;
import java.util.Set;
import java.util.TreeSet;

/**
 * A {@link PolicyEvaluator} that allows exactly the actions on a fixed allowlist and
 * denies everything else with the reason spelled out. Deny-by-default, stateless,
 * side-effect free — the minimal honest policy adapter. A real zone swaps in OPA-wasm
 * or a rules engine by config; the port stays the same.
 */
public final class AllowlistPolicyEvaluator implements PolicyEvaluator {

    private final Set<String> allowedActions;

    /**
     * @param allowedActions the governed action names this evaluator allows;
     *                       everything not listed is denied
     */
    public AllowlistPolicyEvaluator(Set<String> allowedActions) {
        // A sorted copy: immutable, and the deny reason lists the allowlist stably.
        this.allowedActions = Set.copyOf(new TreeSet<>(allowedActions));
    }

    @Override
    public PolicyVerdict evaluate(String subjectId, String action, Map<String, String> attributes) {
        if (allowedActions.contains(action)) {
            return PolicyVerdict.allow();
        }
        return PolicyVerdict.deny(
                "action '" + action + "' is not on the allowlist " + new TreeSet<>(allowedActions));
    }
}
