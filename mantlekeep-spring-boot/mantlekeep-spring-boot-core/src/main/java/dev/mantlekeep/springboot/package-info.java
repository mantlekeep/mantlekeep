/**
 * MantleKeep Spring Boot SDK — shared, transport-agnostic contracts.
 *
 * <p>The sacred SDK surface, split by concern:
 * <ul>
 *   <li>{@code door} — the reactive {@link dev.mantlekeep.springboot.door.DoorClient} adapter
 *       over the pure-JDK spine's {@code Intent}/{@code Decision} value types
 *       ({@code dev.mantlekeep.door.model}): the one-door seam, ONE definition.</li>
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
