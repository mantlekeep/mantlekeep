package dev.mantlekeep.springboot.agent;

import java.util.List;

/**
 * The context handed to the agent when drafting a role: the loop's goal and the outputs
 * of the roles already completed, so the agent builds on what came before. Immutable.
 *
 * @param goal         the loop's overall goal
 * @param priorOutputs outputs of earlier roles, in order (never {@code null})
 */
public record LoopContext(String goal, List<String> priorOutputs) {

    public LoopContext {
        goal = goal == null ? "" : goal;
        priorOutputs = priorOutputs == null ? List.of() : List.copyOf(priorOutputs);
    }
}
