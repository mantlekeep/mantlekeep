/**
 * MantleKeep Spring Boot SDK — shared, transport-agnostic contracts.
 *
 * <p>The sacred SDK surface, split by concern:
 * <ul>
 *   <li>{@code door} — {@link dev.mantlekeep.springboot.door.DoorClient} and its
 *       {@code Intent}/{@code Decision} value objects: the one-door seam.</li>
 *   <li>{@code intent} — {@link dev.mantlekeep.springboot.intent.MantlekeepIntent}: declarative
 *       governance on a method.</li>
 *   <li>{@code agent} — {@link dev.mantlekeep.springboot.agent.AgentPort}: the BYOK seam for
 *       whatever drafts a role's output.</li>
 * </ul>
 *
 * <p>This module depends only on Reactor (for {@code Mono}/{@code Flux} in signatures);
 * it stays framework-free so any starter (webflux, mvc, ai) can build on it.
 */
package dev.mantlekeep.springboot;
