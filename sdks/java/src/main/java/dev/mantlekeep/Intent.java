package dev.mantlekeep;

/** A governed action to submit through the door. Fluent builder. */
public final class Intent {

    private final String action;
    private String resource = "";
    private String env = "";
    private String goal = "";

    private Intent(String action) {
        this.action = action;
    }

    /** Start an intent for an action, e.g. {@code Intent.action("job.promote")}. */
    public static Intent action(String action) {
        return new Intent(action);
    }

    /** The scope this acts on, e.g. {@code "project/demo"} or {@code "run/123"}. */
    public Intent resource(String r) {
        this.resource = r;
        return this;
    }

    /** The target environment for a promote (DEV/SIT/UAT/PROD — your culture). */
    public Intent env(String e) {
        this.env = e;
        return this;
    }

    /** WHY — required by the door (declare-before-execute). */
    public Intent goal(String g) {
        this.goal = g;
        return this;
    }

    String toJson() {
        return "{"
                + "\"action\":\"" + esc(action) + "\","
                + "\"resource\":\"" + esc(resource) + "\","
                + "\"env\":\"" + esc(env) + "\","
                + "\"goal\":\"" + esc(goal) + "\"}";
    }

    private static String esc(String s) {
        return s == null ? "" : s.replace("\\", "\\\\").replace("\"", "\\\"");
    }
}
