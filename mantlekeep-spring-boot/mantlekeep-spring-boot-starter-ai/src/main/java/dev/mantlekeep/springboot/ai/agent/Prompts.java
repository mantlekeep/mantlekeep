package dev.mantlekeep.springboot.ai.agent;

import dev.mantlekeep.springboot.agent.LoopContext;
import dev.mantlekeep.springboot.agent.Role;
import java.util.List;

/**
 * The loop's prompts, in one place so every adapter sends the SAME text.
 *
 * <p>This matters more than it looks: if the CLI adapter and an HTTP model adapter phrase the
 * task differently, their drafts are not comparable and "swap the model, keep the loop" stops
 * being true. The prompt is part of the loop's contract, not an adapter's private business.
 */
final class Prompts {

    private Prompts() {}

    /**
     * Build the prompt for drafting a role. The role frames who the agent is; the goal and
     * prior outputs give it what came before so it builds on the work rather than starting cold.
     */
    static String draft(Role role, LoopContext context) {
        StringBuilder prompt = new StringBuilder()
                .append("You are the \"").append(role.name())
                .append("\" role in a governed engineering loop.\n\n");

        if (!role.description().isBlank()) {
            prompt.append(role.description()).append("\n\n");
        }
        if (!context.goal().isBlank()) {
            prompt.append("Loop goal: ").append(context.goal()).append("\n\n");
        }

        List<String> prior = context.priorOutputs();
        if (!prior.isEmpty()) {
            prompt.append("Outputs of the roles completed so far:\n");
            for (int i = 0; i < prior.size(); i++) {
                prompt.append(i + 1).append(". ").append(prior.get(i)).append('\n');
            }
            prompt.append('\n');
        }

        if (role.iterative()) {
            // Without knowing where it is, every pass starts the work over — and the reviewer
            // gets five commits that each rebuild the same thing.
            prompt.append("This is pass ").append(role.pass()).append(" of ")
                    .append(role.passes()).append(".\n")
                    .append("Do ONE item of the plan, the next one not yet done. Read what is ")
                    .append("already in the working directory before writing: earlier passes ")
                    .append("left their work there.\n")
                    .append("End your reply with exactly MORE if items remain, or DONE if the ")
                    .append("plan is complete.\n\n");
        }
        if (role.mustProduce()) {
            // A role that must leave files behind has to be told so. Without this it returns
            // prose and then fails for a reason it was never given a chance to avoid.
            prompt.append("WRITE FILES. Your working directory is a workspace prepared for ")
                    .append("this step; create the files there, at their real paths.\n")
                    .append("This role must produce: ").append(String.join(", ", role.produces()))
                    .append("\nA step that writes nothing FAILS, whatever you say about it — ")
                    .append("so do not describe the files, create them.\n\n")
                    .append("Then reply with a SHORT summary of what you wrote. The files are ")
                    .append("the deliverable; the summary is only a note about them.");
            return prompt.toString();
        }
        return prompt.append("Produce only the ").append(role.name())
                .append(" output, nothing else.").toString();
    }

    /** Build the critic prompt: judge the work so far and CONTINUE or REVISIT a named role. */
    static String critique(LoopContext context, List<String> revisitableRoles) {
        StringBuilder p = new StringBuilder()
                .append("You are a DEMANDING senior reviewer in a governed engineering loop. These drafts ")
                .append("were written fast and are often thin. Catch when an EARLIER role has a real gap: a ")
                .append("missing decision, an unhandled case, or an assumption the later work exposes as wrong.\n\n");
        if (!context.goal().isBlank()) {
            p.append("Loop goal: ").append(context.goal()).append("\n\n");
        }
        List<String> prior = context.priorOutputs();
        if (!prior.isEmpty()) {
            p.append("Work completed so far (in order):\n");
            for (int i = 0; i < prior.size(); i++) {
                p.append(i + 1).append(". ").append(prior.get(i)).append('\n');
            }
            p.append('\n');
        }
        p.append("Roles you may send back for rework: ")
                .append(String.join(", ", revisitableRoles)).append("\n\n");
        p.append("If ANY earlier role has a concrete, fixable gap (a missing decision, an unhandled case, ")
                .append("an assumption later work contradicts), send it back — reply with EXACTLY one line:\n")
                .append("REVISIT: <role> — <the specific gap>\n")
                .append("Only if the earlier roles are genuinely solid, reply exactly: CONTINUE\n")
                .append("Prefer catching a real gap over waving it through. Reply with only that one line.");
        return p.toString();
    }
}
