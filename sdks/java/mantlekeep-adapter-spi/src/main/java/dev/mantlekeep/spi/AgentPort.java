package dev.mantlekeep.spi;

/**
 * The AI-agent port — how a governed host drives an agent harness (internal LLM,
 * external API behind the mandatory proxy, or none at all — BYOK, the host chooses).
 * Every tool call the agent makes still goes back through the door; this port only
 * abstracts WHICH harness thinks, never whether it is governed.
 */
public interface AgentPort {

    /**
     * Runs one agent task to completion under governance.
     *
     * @param agentTaskJson the task (JSON): goal, context, allowed-tool set
     * @return the agent's result (JSON): output plus the tool-call trail
     */
    String run(String agentTaskJson);
}
