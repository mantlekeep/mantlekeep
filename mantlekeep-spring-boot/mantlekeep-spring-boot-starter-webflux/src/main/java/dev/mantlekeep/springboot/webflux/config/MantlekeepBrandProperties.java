package dev.mantlekeep.springboot.webflux.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Binds {@code mantlekeep.brand.*} to the product's brand.
 *
 * <p>Kept separate from the core {@link dev.mantlekeep.springboot.brand.Brand} value the way
 * {@link MantlekeepDoorProperties} is kept separate from {@code DoorProperties}: the binding class
 * is a framework concern, the value it produces is not. Relaxed binding maps each field to the
 * matching {@code MANTLEKEEP_BRAND_*} environment variable, so a build branded through env vars —
 * the MantleKeep wrapper's mechanism — needs no code or YAML.
 *
 * @param name    {@code mantlekeep.brand.name}    ← {@code MANTLEKEEP_BRAND_NAME}
 * @param mark    {@code mantlekeep.brand.mark}    ← {@code MANTLEKEEP_BRAND_MARK}
 * @param tagline {@code mantlekeep.brand.tagline} ← {@code MANTLEKEEP_BRAND_TAGLINE}
 * @param kicker  {@code mantlekeep.brand.kicker}  ← {@code MANTLEKEEP_BRAND_KICKER}
 */
@ConfigurationProperties(prefix = "mantlekeep.brand")
public record MantlekeepBrandProperties(String name, String mark, String tagline, String kicker) {
}
