package dev.mantlekeep.springboot.ai.config;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Binds {@code mantlekeep.agent.claude.*} (constructor binding) with safe defaults.
 *
 * <pre>
 * mantle:
 *   agent:
 *     claude:
 *       binary: claude   # the CLI on PATH (default)
 *       timeout: 120s     # per-draft budget before the process is killed
 * </pre>
 *
 * @param binary  the Claude Code CLI binary to launch (default {@code claude})
 * @param timeout how long a single draft may run (default 120s)
 */
@ConfigurationProperties(prefix = "mantlekeep.agent.claude")
public record ClaudeProperties(String binary, Duration timeout) {

    public ClaudeProperties {
        binary = (binary == null || binary.isBlank()) ? "claude" : binary;
        timeout = timeout == null ? Duration.ofSeconds(120) : timeout;
    }
}
