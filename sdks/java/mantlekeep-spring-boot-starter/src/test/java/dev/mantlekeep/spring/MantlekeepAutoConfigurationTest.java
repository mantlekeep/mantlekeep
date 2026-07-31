package dev.mantlekeep.spring;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.DoorClient;
import dev.mantlekeep.door.ServiceDoorClient;
import dev.mantlekeep.door.model.DoorMode;
import org.junit.jupiter.api.Test;
import org.springframework.boot.autoconfigure.AutoConfigurations;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;

/**
 * Proves the no-code flow: properties in, DoorClient bean + aspect out — and
 * complete inertness when {@code mantlekeep.door.mode} is absent.
 */
class MantlekeepAutoConfigurationTest {

    private final ApplicationContextRunner contextRunner = new ApplicationContextRunner()
            .withConfiguration(AutoConfigurations.of(MantlekeepAutoConfiguration.class));

    @Test
    void serviceModePropertiesYieldTheWireClientAndTheAspect() {
        contextRunner
                .withPropertyValues(
                        "mantlekeep.door.mode=service",
                        "mantlekeep.door.url=http://localhost:8080",
                        "mantlekeep.door.subject=lead-bob")
                .run(context -> {
                    assertInstanceOf(ServiceDoorClient.class, context.getBean(DoorClient.class));
                    assertTrue(context.containsBean("mantleIntentAspect"));
                    assertEquals("lead-bob",
                            context.getBean(SubjectResolver.class).currentSubject().id());
                });
    }

    @Test
    void withoutTheModePropertyTheStarterIsInert() {
        contextRunner.run(context ->
                assertFalse(context.containsBean("doorClient"),
                        "no mantlekeep.door.mode → no governance beans, no surprises"));
    }

    @Test
    void propertyDefaultsMatchTheRecordDefaults() {
        contextRunner
                .withPropertyValues(
                        "mantlekeep.door.mode=service",
                        "mantlekeep.door.url=http://localhost:8080")
                .run(context -> {
                    MantlekeepProperties properties = context.getBean(MantlekeepProperties.class);
                    assertEquals("mantlekeep", properties.brand(), "the brand default is the rebrand seam");
                    assertEquals(DoorMode.SERVICE, properties.mode());
                    assertTrue(properties.adapters().isEmpty());
                });
    }
}
