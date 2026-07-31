package dev.mantlekeep.springboot.ai.config;

import dev.mantlekeep.springboot.agent.AgentPort;
import dev.mantlekeep.springboot.ai.agent.ClaudeRunner;
import dev.mantlekeep.springboot.ai.agent.HarnessAgent;
import dev.mantlekeep.springboot.ai.agent.HttpChatAgent;
import dev.mantlekeep.springboot.ai.agent.ProcessClaudeRunner;
import java.net.URI;
import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;

/**
 * Auto-configuration for the BYOK agent adapters. {@code mantlekeep.agent.provider} selects one:
 *
 * <ul>
 *   <li>{@code claude-cli} (default) — {@link HarnessAgent} over the local Claude Code CLI.
 *       Needs the binary on the server, which many regulated environments will not allow.</li>
 *   <li>{@code http} — {@link HttpChatAgent} against an OpenAI-compatible endpoint (Azure
 *       OpenAI, self-hosted vLLM/Ollama, a gateway). This is the adapter for an organisation
 *       that has a model endpoint but will not put a CLI on a server.</li>
 * </ul>
 *
 * Every bean is {@link ConditionalOnMissingBean}, so an application can still supply its own
 * {@link AgentPort} and neither adapter is in the way. The model choice is configuration, and
 * the loop never learns which one it got.
 */
@AutoConfiguration
@EnableConfigurationProperties({ClaudeProperties.class, ChatModelProperties.class})
public class MantlekeepAgentAutoConfiguration {

    @Bean
    @ConditionalOnMissingBean
    @ConditionalOnProperty(name = "mantlekeep.agent.provider", havingValue = "claude-cli", matchIfMissing = true)
    public ClaudeRunner claudeRunner(ClaudeProperties properties) {
        return new ProcessClaudeRunner(properties.binary(), properties.timeout());
    }

    @Bean
    @ConditionalOnMissingBean
    @ConditionalOnProperty(name = "mantlekeep.agent.provider", havingValue = "claude-cli", matchIfMissing = true)
    public AgentPort harnessAgent(ClaudeRunner claudeRunner) {
        return new HarnessAgent(claudeRunner);
    }

    @Bean
    @ConditionalOnMissingBean
    @ConditionalOnProperty(name = "mantlekeep.agent.provider", havingValue = "http")
    public AgentPort httpChatAgent(ChatModelProperties properties) {
        if (properties.url() == null || properties.url().isBlank()) {
            throw new IllegalStateException(
                    "mantlekeep.agent.provider=http requires mantlekeep.agent.model.url "
                            + "(the full chat-completions endpoint)");
        }
        return new HttpChatAgent(
                URI.create(properties.url()),
                properties.model(),
                properties.apiKey(),
                properties.authHeader(),
                properties.authPrefix(),
                properties.timeout());
    }
}
