package dev.mantlekeep.spring;

import java.util.HashMap;
import java.util.Map;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.env.EnvironmentPostProcessor;
import org.springframework.core.env.ConfigurableEnvironment;
import org.springframework.core.env.EnumerablePropertySource;
import org.springframework.core.env.MapPropertySource;
import org.springframework.core.env.PropertySource;

/**
 * Lets a white-label product configure MantleKeep under its OWN property prefix, so its
 * configuration files never mention the framework.
 *
 * <p>This is the Spring-side counterpart of the core's environment-prefix remap. Without
 * it the seam was only half-present: a branded binary could hide the framework from
 * operators setting environment variables, but every {@code application.yml} still had
 * to say {@code mantlekeep:} — the one file a reviewer is most likely to open.
 *
 * <p>Set the prefix once, before the application starts:
 *
 * <pre>{@code
 * BrandPrefix.use("acme");            // or MANTLEKEEP_BRAND_PREFIX=acme
 * SpringApplication.run(App.class, args);
 * }</pre>
 *
 * <p>and configuration becomes yours:
 *
 * <pre>{@code
 * acme:
 *   door:
 *     mode: service
 *     url: https://door.internal
 * }</pre>
 *
 * <p>Aliases are added as the LOWEST-precedence property source, so an explicit
 * {@code mantlekeep.*} value always wins — adopting a brand prefix can never silently
 * change the meaning of configuration that was already there.
 */
public class BrandPrefixEnvironmentPostProcessor implements EnvironmentPostProcessor {

    /** The framework's own prefix — the target every alias maps onto. */
    private static final String FRAMEWORK_PREFIX = "mantlekeep.";

    private static final String ALIAS_SOURCE_NAME = "mantlekeep-brand-aliases";

    @Override
    public void postProcessEnvironment(
            ConfigurableEnvironment environment, SpringApplication application) {
        String brandPrefix = BrandPrefix.resolve(environment);
        if (brandPrefix == null || brandPrefix.isBlank()) {
            return; // no brand configured — the framework's own prefix is used directly
        }

        String prefixWithDot = brandPrefix.endsWith(".") ? brandPrefix : brandPrefix + ".";
        Map<String, Object> aliases = new HashMap<>();
        for (PropertySource<?> source : environment.getPropertySources()) {
            if (!(source instanceof EnumerablePropertySource<?> enumerable)) {
                continue; // a non-enumerable source cannot be scanned for keys
            }
            for (String propertyName : enumerable.getPropertyNames()) {
                if (propertyName.startsWith(prefixWithDot)) {
                    String frameworkName =
                            FRAMEWORK_PREFIX + propertyName.substring(prefixWithDot.length());
                    aliases.putIfAbsent(frameworkName, environment.getProperty(propertyName));
                }
            }
        }

        if (!aliases.isEmpty()) {
            // addLast → lowest precedence, so an explicit mantlekeep.* value still wins.
            environment.getPropertySources().addLast(new MapPropertySource(ALIAS_SOURCE_NAME, aliases));
        }
    }
}
