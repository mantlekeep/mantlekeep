package dev.mantlekeep.springboot.brand;

/**
 * The name a governed product wears in front of an operator — its label, never the engine's.
 *
 * <p>The SDK is the sovereign engine, the way the Go core is: sealed, and called "MantleKeep"
 * underneath. A white-label product built on it (a host's "MantleKeep") must not leak that
 * name to a console, a log line, or an API — the same rule the Go {@code MANTLEKEEP_BRAND_*}
 * wrapper already enforces for the core. This value carries the product's own label so nothing
 * has to hard-code "MantleKeep" as a string literal, which is exactly where such leaks hide.
 *
 * <p>The defaults are "MantleKeep": an unbranded build is honestly MantleKeep, and a branded one sets
 * the fields. Bound from {@code mantlekeep.brand.*} — and therefore, by Spring's relaxed binding,
 * from the same {@code MANTLEKEEP_BRAND_NAME} / {@code MANTLEKEEP_BRAND_MARK} environment variables the
 * Go wrapper sets, so one operator can brand the whole stack, Go and Java, with one set of vars.
 *
 * @param name    the product name shown to an operator (default {@code MantleKeep})
 * @param mark    a short glyph that precedes the name (default {@code 🔱})
 * @param tagline one line under the name (default the platform's own)
 * @param kicker  an optional eyebrow above the name; empty by default
 */
public record Brand(String name, String mark, String tagline, String kicker) {

    /** The engine's own identity — the value when nothing is branded. */
    public static final Brand DEFAULT = new Brand(null, null, null, null);

    public Brand {
        name = blankTo(name, "MantleKeep");
        mark = blankTo(mark, "🔱"); // 🔱
        tagline = blankTo(tagline, "the one door — human + AI, every action governed");
        kicker = kicker == null ? "" : kicker.trim();
    }

    private static String blankTo(String value, String fallback) {
        return value == null || value.isBlank() ? fallback : value.trim();
    }
}
