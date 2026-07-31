package dev.mantlekeep.springboot.ai.agent;

import dev.mantlekeep.springboot.agent.AgentPort;
import dev.mantlekeep.springboot.agent.LoopContext;
import dev.mantlekeep.springboot.agent.Role;
import java.util.List;
import java.util.Objects;
import reactor.core.publisher.Flux;
import reactor.core.publisher.Mono;

/**
 * An {@link AgentPort} that drafts a role's output by asking the local Claude Code CLI,
 * running headless. This adapter owns only prompt-building; the actual process launch is
 * delegated to an injected {@link ClaudeRunner}, which keeps the agent testable without a
 * real {@code claude} on the path.
 *
 * <p>It streams too: {@link #draftStream} delegates to {@link ClaudeRunner#stream}, so a
 * streaming runner (the default {@link ProcessClaudeRunner}) surfaces the draft token by
 * token for a live "watch it type" experience over SSE.
 */
public final class HarnessAgent implements AgentPort {

    private final ClaudeRunner runner;

    /**
     * @param runner the CLI runner to delegate to (required)
     */
    public HarnessAgent(ClaudeRunner runner) {
        this.runner = Objects.requireNonNull(runner, "runner is required");
    }

    @Override
    public Mono<String> draft(Role role, LoopContext context) {
        Objects.requireNonNull(role, "role is required");
        Objects.requireNonNull(context, "context is required");
        return runner.run(buildPrompt(role, context));
    }

    @Override
    public Flux<String> draftStream(Role role, LoopContext context) {
        Objects.requireNonNull(role, "role is required");
        Objects.requireNonNull(context, "context is required");
        return runner.stream(buildPrompt(role, context));
    }

    /**
     * Draft with the harness running IN the granted workspace, so files it writes land where
     * the platform can inspect them afterwards. This is what makes an artifact observable
     * rather than merely claimed.
     */
    @Override
    public Flux<String> draftStream(Role role, LoopContext context, java.nio.file.Path workspace) {
        return runner.stream(Prompts.draft(role, context), workspace);
    }

    @Override
    public Mono<String> draft(Role role, LoopContext context, java.nio.file.Path workspace) {
        return runner.run(Prompts.draft(role, context), workspace);
    }

    /** The harness is a real process with a real filesystem, so a grant means something. */
    @Override
    public boolean canWorkInAWorkspace() {
        return true;
    }

    @Override
    public String identity() {
        return "claude-cli";
    }

    @Override
    public Mono<String> critique(LoopContext context, List<String> revisitableRoles) {
        Objects.requireNonNull(context, "context is required");
        if (revisitableRoles == null || revisitableRoles.isEmpty()) {
            return Mono.just("CONTINUE");
        }
        return runner.run(buildCritiquePrompt(context, revisitableRoles));
    }

    /** The loop's critic prompt — shared with every other adapter (see {@link Prompts}). */
    static String buildCritiquePrompt(LoopContext context, List<String> revisitableRoles) {
        return Prompts.critique(context, revisitableRoles);
    }

    /** The loop's draft prompt — shared with every other adapter (see {@link Prompts}). */
    static String buildPrompt(Role role, LoopContext context) {
        return Prompts.draft(role, context);
    }
}
