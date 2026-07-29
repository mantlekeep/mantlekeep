package dev.mantlekeep.spring;

import org.springframework.core.env.Environment;

/**
 * The property prefix a white-label product configures MantleKeep under.
 *
 * <p>Declare it once in the product's launcher, before the application starts:
 *
 * <pre>{@code
 * public static void main(String[] args) {
 *     BrandPrefix.use("acme");
 *     SpringApplication.run(AcmeApplication.class, args);
 * }
 * }</pre>
 *
 * <p>From then on the product's configuration files speak its own name
 * ({@code acme.door.url}) and never the framework's. Operators can set it instead as
 * {@code MANTLEKEEP_BRAND_PREFIX}, which is useful when the same image is rebranded per
 * deployment rather than per build.
 *
 * @see BrandPrefixEnvironmentPostProcessor for how the aliasing is applied
 */
public final class BrandPrefix {

    /** The property/environment key naming the brand prefix. */
    public static final String PROPERTY = "mantlekeep.brand.prefix";

    private BrandPrefix() {}

    /**
     * Declares the prefix this product configures MantleKeep under. Call before
     * {@code SpringApplication.run} — property sources are read as the context starts,
     * so a later call would come too late to take effect.
     *
     * @param prefix the product's own prefix, e.g. {@code "acme"}
     */
    public static void use(String prefix) {
        if (prefix == null || prefix.isBlank()) {
            throw new IllegalArgumentException("brand prefix must not be blank");
        }
        System.setProperty(PROPERTY, prefix);
    }

    /**
     * Resolves the configured prefix. The environment is consulted first so an operator
     * can override what the build chose; the system property set by {@link #use} is the
     * fallback.
     *
     * @return the prefix, or {@code null} when no brand is configured
     */
    static String resolve(Environment environment) {
        String fromEnvironment = environment.getProperty(PROPERTY);
        if (fromEnvironment != null && !fromEnvironment.isBlank()) {
            return fromEnvironment;
        }
        return System.getProperty(PROPERTY);
    }
}
