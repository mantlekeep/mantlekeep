package dev.mantlekeep.door;

import java.util.ArrayList;
import java.util.List;

/**
 * A tiny in-memory stand-in for the native core: one deny rule (actions listed as
 * denied), everything else allowed, every decision appended to an in-memory chain.
 * Exists so the embedded path is FULLY testable with no native library anywhere —
 * exactly what the {@link NativeCore} port is for.
 */
final class InMemoryNativeCore implements NativeCore {

    private final List<String> deniedActions;
    private final List<String> chain = new ArrayList<>();
    private boolean closed;

    InMemoryNativeCore(List<String> deniedActions) {
        this.deniedActions = List.copyOf(deniedActions);
    }

    @Override
    public String submitJson(String intentJson) {
        String action = JsonText.stringField(intentJson, "action");
        String intentId = JsonText.stringField(intentJson, "id");
        String subjectId = JsonText.stringField(intentJson, "subject_id");
        boolean denied = deniedActions.contains(action);
        chain.add("{"
                + "\"intent_id\":" + JsonText.quote(intentId) + ","
                + "\"subject_id\":" + JsonText.quote(subjectId) + ","
                + "\"action\":" + JsonText.quote(action) + ","
                + "\"decision\":" + JsonText.quote(denied ? "deny" : "allow")
                + "}");
        // Emit the canonical native Decision contract (docs/native-core-contract.md):
        // snake_case on the FFI boundary, the SAME logical shape the HTTP wire carries.
        // This test double IS the reference a real c-shared/Rust core is parity-checked
        // against, so it must speak the real contract, not a shorthand.
        return denied
                ? "{\"outcome\":\"deny\",\"policy_id\":\"policy.test.allowlist\",\"required_approvers\":[],"
                        + "\"reasons\":[{\"code\":\"DENY_ACTION_NOT_ALLOWED\","
                        + "\"message\":\"action '" + action + "' is denied by test policy\"}]}"
                : "{\"outcome\":\"allow\",\"token\":\"TOK-" + chain.size() + "\","
                        + "\"policy_id\":\"policy.test.allowlist\",\"expires_at\":\"2026-08-01T00:00:00Z\","
                        + "\"reasons\":[]}";
    }

    @Override
    public String auditJson() {
        return "[" + String.join(",", chain) + "]";
    }

    @Override
    public boolean verifyChain() {
        return true;
    }

    @Override
    public void close() {
        closed = true;
    }

    boolean isClosed() {
        return closed;
    }
}
