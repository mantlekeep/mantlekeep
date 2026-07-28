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
        return denied
                ? "{\"action\":\"deny\",\"reason\":\"action '" + action + "' is denied by test policy\"}"
                : "{\"action\":\"allow\",\"token\":\"TOK-" + chain.size() + "\"}";
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
