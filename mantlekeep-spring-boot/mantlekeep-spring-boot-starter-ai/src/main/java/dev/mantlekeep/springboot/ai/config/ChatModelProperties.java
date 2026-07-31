package dev.mantlekeep.springboot.ai.config;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Binds {@code mantlekeep.agent.model.*} — the SERVER-SIDE model endpoint, used when
 * {@code mantlekeep.agent.provider=http}.
 *
 * <pre>
 * mantle:
 *   agent:
 *     provider: http
 *     model:
 *       url: https://my-resource.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=2024-02-01
 *       model: gpt-4o
 *       api-key: ${MODEL_API_KEY}
 *       auth-header: api-key      # Azure; omit for OpenAI-style Bearer auth
 *       auth-prefix: ""           # Azure sends the raw key
 *       timeout: 120s
 * </pre>
 *
 * <p>{@code url} is the FULL chat-completions endpoint rather than a base URL, because the
 * path differs per vendor (Azure embeds the deployment and an api-version; OpenAI and most
 * gateways use {@code /v1/chat/completions}). One property covers all of them.
 *
 * @param url        the full chat-completions endpoint
 * @param model      the model or deployment name sent in the request body
 * @param apiKey     the credential (leave blank for an unauthenticated local endpoint)
 * @param authHeader header carrying the credential (default {@code Authorization})
 * @param authPrefix prefix before the credential (default {@code "Bearer "}; Azure uses none)
 * @param timeout    per-request budget (default 120s)
 */
@ConfigurationProperties(prefix = "mantlekeep.agent.model")
public record ChatModelProperties(
        String url,
        String model,
        String apiKey,
        String authHeader,
        String authPrefix,
        Duration timeout) {

    public ChatModelProperties {
        model = (model == null || model.isBlank()) ? "gpt-4o-mini" : model;
        apiKey = apiKey == null ? "" : apiKey;
        authHeader = (authHeader == null || authHeader.isBlank()) ? "Authorization" : authHeader;
        authPrefix = authPrefix == null ? "Bearer " : authPrefix;
        timeout = timeout == null ? Duration.ofSeconds(120) : timeout;
    }
}
