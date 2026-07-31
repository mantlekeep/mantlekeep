package dev.mantlekeep.springboot.agent;

/**
 * A role the agent drafts for — one governed step in the loop (spec, build, review,
 * security, evidence…). Immutable.
 *
 * @param name        the role's name (required)
 * @param description what the role should produce (may be empty)
 * @param pass        which pass this is, and of how many — {@code 2 of 5}. A role working
 *                    through a plan an item at a time needs to know where it is, or every pass
 *                    starts the work again.
 * @param produces    file patterns this role must leave behind, e.g. {@code **}{@code /*.html}.
 *                    Empty means the role produces TEXT, where the output is the artifact.
 *                    <p>The agent has to be TOLD this. A role that must write files but is
 *                    asked only to "draft" will return prose — correctly, given what it was
 *                    asked — and then fail for a reason it had no way to avoid.
 */
public record Role(String name, String description, java.util.List<String> produces,
        int pass, int passes) {

    public Role {
        if (name == null || name.isBlank()) {
            throw new IllegalArgumentException("role name is required");
        }
        description = description == null ? "" : description;
        produces = produces == null ? java.util.List.of() : java.util.List.copyOf(produces);
        pass = Math.max(pass, 1);
        passes = Math.max(passes, 1);
    }

    /** A role that produces text — the common case, and the previous behaviour. */
    public Role(String name, String description) {
        this(name, description, java.util.List.of(), 1, 1);
    }

    /** A role that must leave files behind, drafted in one pass. */
    public Role(String name, String description, java.util.List<String> produces) {
        this(name, description, produces, 1, 1);
    }

    /** True when this role works through its plan across several passes. */
    public boolean iterative() {
        return passes > 1;
    }

    /** True when this role must leave files behind for the step to have succeeded. */
    public boolean mustProduce() {
        return !produces.isEmpty();
    }
}
