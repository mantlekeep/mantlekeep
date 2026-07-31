package dev.mantlekeep.springboot.door;

import java.util.Map;

/**
 * An intent: what a caller asks the one door to authorize. Immutable.
 *
 * <p>Identity is deliberately NOT carried here — it flows from the authenticated caller
 * at the door (verified from the SSO assertion, never trusted from a request body). An
 * {@code Intent} therefore describes only the action asked for, not who asks it.
 *
 * @param action   the governed action, e.g. {@code "loop.propose"} (required)
 * @param resource the scope target, e.g. {@code "loop/LOOP-0001"} (may be empty)
 * @param goal     the human intent behind the action (required — the door demands one)
 * @param env      the target environment, e.g. {@code "sit"} (may be empty)
 * @param scope    a further scope qualifier (may be empty)
 * @param params   action-specific parameters (never {@code null}; defensively copied)
 */
public record Intent(
        String action,
        String resource,
        String goal,
        String env,
        String scope,
        Map<String, Object> params) {

    public Intent {
        if (action == null || action.isBlank()) {
            throw new IllegalArgumentException("intent action is required");
        }
        if (goal == null || goal.isBlank()) {
            throw new IllegalArgumentException("intent goal is required");
        }
        resource = resource == null ? "" : resource;
        env = env == null ? "" : env;
        scope = scope == null ? "" : scope;
        params = params == null ? Map.of() : Map.copyOf(params);
    }

    /** Start building an intent for the given action. */
    public static Builder of(String action) {
        return new Builder(action);
    }

    /** Fluent builder for the optional fields — keeps call sites readable. */
    public static final class Builder {
        private final String action;
        private String resource = "";
        private String goal = "";
        private String env = "";
        private String scope = "";
        private Map<String, Object> params = Map.of();

        private Builder(String action) {
            this.action = action;
        }

        public Builder resource(String resource) {
            this.resource = resource;
            return this;
        }

        public Builder goal(String goal) {
            this.goal = goal;
            return this;
        }

        public Builder env(String env) {
            this.env = env;
            return this;
        }

        public Builder scope(String scope) {
            this.scope = scope;
            return this;
        }

        public Builder params(Map<String, Object> params) {
            this.params = params;
            return this;
        }

        public Intent build() {
            return new Intent(action, resource, goal, env, scope, params);
        }
    }
}
