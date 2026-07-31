/**
 * MantleKeep Spring Boot reactive starter (WebFlux). Organised by concern:
 * <ul>
 *   <li>{@code door} — the reactive {@code WebClient}-backed
 *       {@link dev.mantlekeep.springboot.door.DoorClient} implementation + its wire DTO.</li>
 *   <li>{@code config} — {@code mantlekeep.door.*} binding + auto-configuration.</li>
 *   <li>{@code intent} — the reactive {@code @MantlekeepIntent} aspect: govern-then-proceed.</li>
 * </ul>
 */
package dev.mantlekeep.springboot.webflux;
