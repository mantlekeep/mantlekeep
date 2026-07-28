package dev.mantlekeep.springboot.ai.config;

import static org.assertj.core.api.Assertions.assertThat;

import dev.mantlekeep.springboot.agent.AgentPort;
import dev.mantlekeep.springboot.ai.agent.ClaudeRunner;
import dev.mantlekeep.springboot.ai.agent.HarnessAgent;
import java.time.Duration;
import org.junit.jupiter.api.Test;
import org.springframework.boot.autoconfigure.AutoConfigurations;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;
import reactor.core.publisher.Mono;

/**
 * Wiring tests for {@link MantlekeepAgentAutoConfiguration} using
 * {@link ApplicationContextRunner} — no context bootstrap of a full application.
 */
class MantlekeepAgentAutoConfigurationTest {

    private final ApplicationContextRunner runner = new ApplicationContextRunner()
            .withConfiguration(AutoConfigurations.of(MantlekeepAgentAutoConfiguration.class));

    @Test
    void wiresHarnessAgentAsAgentPort() {
        runner.run(ctx -> {
            assertThat(ctx).hasSingleBean(AgentPort.class);
            assertThat(ctx).hasSingleBean(ClaudeRunner.class);
            assertThat(ctx.getBean(AgentPort.class)).isInstanceOf(HarnessAgent.class);
        });
    }

    @Test
    void appliesSafeDefaultsWhenUnconfigured() {
        runner.run(ctx -> {
            ClaudeProperties props = ctx.getBean(ClaudeProperties.class);
            assertThat(props.binary()).isEqualTo("claude");
            assertThat(props.timeout()).isEqualTo(Duration.ofSeconds(120));
        });
    }

    @Test
    void bindsConfiguredBinaryAndTimeout() {
        runner.withPropertyValues(
                        "mantlekeep.agent.claude.binary=/opt/claude",
                        "mantlekeep.agent.claude.timeout=30s")
                .run(ctx -> {
                    ClaudeProperties props = ctx.getBean(ClaudeProperties.class);
                    assertThat(props.binary()).isEqualTo("/opt/claude");
                    assertThat(props.timeout()).isEqualTo(Duration.ofSeconds(30));
                });
    }

    @Test
    void backsOffWhenApplicationDefinesOwnAgentPort() {
        AgentPort custom = (role, context) -> Mono.just("custom");
        runner.withBean("customAgent", AgentPort.class, () -> custom)
                .run(ctx -> {
                    assertThat(ctx).getBeans(AgentPort.class).hasSize(1);
                    assertThat(ctx.getBean(AgentPort.class)).isSameAs(custom);
                });
    }

    @Test
    void backsOffWhenApplicationDefinesOwnClaudeRunner() {
        ClaudeRunner custom = prompt -> Mono.just("stub");
        runner.withBean("customRunner", ClaudeRunner.class, () -> custom)
                .run(ctx -> assertThat(ctx.getBean(ClaudeRunner.class)).isSameAs(custom));
    }
}
