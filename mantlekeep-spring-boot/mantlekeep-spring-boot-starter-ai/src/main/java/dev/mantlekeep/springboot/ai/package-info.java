/**
 * MantleKeep Spring Boot AI starter. An {@link dev.mantlekeep.springboot.agent.AgentPort}
 * adapter that drafts a role's output by invoking the local Claude Code CLI headless
 * ({@code claude -p "<prompt>" --output-format text}). Organised by concern:
 * <ul>
 *   <li>{@code agent} — the {@link dev.mantlekeep.springboot.ai.agent.HarnessAgent} adapter,
 *       the injectable {@link dev.mantlekeep.springboot.ai.agent.ClaudeRunner} seam, and its
 *       {@link dev.mantlekeep.springboot.ai.agent.ProcessClaudeRunner} default.</li>
 *   <li>{@code config} — {@code mantlekeep.agent.claude.*} binding + auto-configuration.</li>
 * </ul>
 * The CLI is one adapter behind the port; model choice stays a swappable concern.
 */
package dev.mantlekeep.springboot.ai;
