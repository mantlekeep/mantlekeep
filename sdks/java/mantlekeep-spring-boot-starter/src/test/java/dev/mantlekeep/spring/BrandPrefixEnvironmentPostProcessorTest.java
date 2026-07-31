package dev.mantlekeep.spring;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.springframework.mock.env.MockEnvironment;

/**
 * The contract a white-label product depends on: its configuration files name ITS
 * prefix, never the framework's — and adopting a prefix can never silently change
 * configuration that was already there.
 */
class BrandPrefixEnvironmentPostProcessorTest {

    private final BrandPrefixEnvironmentPostProcessor processor =
            new BrandPrefixEnvironmentPostProcessor();

    @AfterEach
    void clearBrandPrefix() {
        System.clearProperty(BrandPrefix.PROPERTY);
    }

    @Test
    void brandPrefixedPropertiesBindToTheFramework() {
        MockEnvironment environment = new MockEnvironment()
                .withProperty(BrandPrefix.PROPERTY, "acme")
                .withProperty("acme.door.mode", "service")
                .withProperty("acme.door.url", "https://door.internal");

        processor.postProcessEnvironment(environment, null);

        assertEquals("service", environment.getProperty("mantlekeep.door.mode"));
        assertEquals("https://door.internal", environment.getProperty("mantlekeep.door.url"));
    }

    @Test
    void anExplicitFrameworkPropertyWinsOverItsBrandAlias() {
        // Precedence matters: adopting a brand prefix must not override configuration
        // that was already stated in the framework's own namespace.
        MockEnvironment environment = new MockEnvironment()
                .withProperty(BrandPrefix.PROPERTY, "acme")
                .withProperty("acme.door.url", "https://from-brand")
                .withProperty("mantlekeep.door.url", "https://explicit");

        processor.postProcessEnvironment(environment, null);

        assertEquals("https://explicit", environment.getProperty("mantlekeep.door.url"));
    }

    @Test
    void withNoBrandConfiguredNothingIsAliased() {
        MockEnvironment environment = new MockEnvironment().withProperty("acme.door.url", "https://x");

        processor.postProcessEnvironment(environment, null);

        assertNull(environment.getProperty("mantlekeep.door.url"));
    }

    @Test
    void theLauncherCanDeclareThePrefixBeforeStartup() {
        BrandPrefix.use("acme");
        MockEnvironment environment = new MockEnvironment().withProperty("acme.door.mode", "embedded");

        processor.postProcessEnvironment(environment, null);

        assertEquals("embedded", environment.getProperty("mantlekeep.door.mode"));
    }
}
