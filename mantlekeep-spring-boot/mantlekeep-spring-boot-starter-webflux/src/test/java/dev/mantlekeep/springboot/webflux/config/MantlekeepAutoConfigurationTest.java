package dev.mantlekeep.springboot.webflux.config;

import static org.assertj.core.api.Assertions.assertThat;

import dev.mantlekeep.springboot.door.DoorClient;
import dev.mantlekeep.springboot.door.DoorProperties;
import org.junit.jupiter.api.Test;
import org.springframework.boot.autoconfigure.AutoConfigurations;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;

class MantlekeepAutoConfigurationTest {

    private final ApplicationContextRunner runner = new ApplicationContextRunner()
            .withConfiguration(AutoConfigurations.of(MantlekeepAutoConfiguration.class));

    @Test
    void wiresDoorClientAndPropertiesFromConfig() {
        runner.withPropertyValues(
                        "mantlekeep.door.base-url=http://door.example",
                        "mantlekeep.door.bearer-token=secret")
                .run(ctx -> {
                    assertThat(ctx).hasSingleBean(DoorClient.class);
                    assertThat(ctx).hasSingleBean(DoorProperties.class);
                    assertThat(ctx.getBean(DoorProperties.class).baseUrl()).isEqualTo("http://door.example");
                });
    }

    @Test
    void appliesSafeDefaultsWhenUnconfigured() {
        runner.run(ctx -> {
            DoorProperties props = ctx.getBean(DoorProperties.class);
            assertThat(props.baseUrl()).isEqualTo("http://localhost:8080");
            assertThat(props.governPath()).isEqualTo("/api/govern");
        });
    }

    @Test
    void backsOffWhenApplicationDefinesOwnDoorClient() {
        runner.withBean("doorClient", DoorClient.class, () -> intent -> null)
                .run(ctx -> assertThat(ctx).getBeans(DoorClient.class).hasSize(1));
    }
}
