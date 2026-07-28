package dev.mantlekeep.springboot.agent;

import java.nio.file.Path;
import reactor.core.publisher.Flux;
import reactor.core.publisher.Mono;

/**
 * The BYOK seam: whatever produces a role's output implements this — the developer's
 * Claude Code, Spring AI over a model API, or a local model. The loop asks the port to
 * draft; the door governs the result. Model choice is an adapter concern, never
 * hardcoded, so a deployment can swap it (or self-host) without touching the loop.
 */
public interface AgentPort {

    /**
     * Draft a role's output for the given context.
     *
     * @return a {@code Mono} emitting the drafted output
     */
    Mono<String> draft(Role role, LoopContext context);

    /**
     * Stream a role's output as it is produced (for SSE). The default adapts
     * {@link #draft} into a single-element stream; a streaming adapter overrides it.
     */
    default Flux<String> draftStream(Role role, LoopContext context) {
        return draft(role, context).flux();
    }

    /**
     * Draft into a WORKSPACE — a directory the caller has granted this step, which the
     * adapter should run in so anything written lands somewhere the platform owns and can
     * inspect afterwards.
     *
     * <p>Default: ignore it and draft as text. That is the honest behaviour for an adapter
     * with no filesystem — a hosted chat model cannot write files, and pretending otherwise
     * would produce a step that claims artifacts nobody can find. Such an adapter stays
     * text-only rather than broken.
     *
     * @param workingDirectory where the step may work; never {@code null}
     */
    default Flux<String> draftStream(Role role, LoopContext context, Path workingDirectory) {
        return draftStream(role, context);
    }

    /** As {@link #draftStream(Role, LoopContext, Path)}, unstreamed. */
    default Mono<String> draft(Role role, LoopContext context, Path workingDirectory) {
        return draft(role, context);
    }

    /**
     * Whether this adapter actually honours a granted workspace. False by default, because
     * most adapters cannot write anywhere — and a role that must produce artifacts should be
     * refused up front rather than run and fail.
     */
    default boolean canWorkInAWorkspace() {
        return false;
    }

    /**
     * Critique the work so far and decide whether the loop should cycle back to rework an
     * earlier role. Returns either {@code "CONTINUE"} or a line
     * {@code "REVISIT: <role> — <reason>"} naming one of {@code revisitableRoles}. The
     * default never loops back (forward-only); a real agent overrides it. The loop still
     * governs and budget-bounds any revisit, so this can only ever <em>propose</em> a cycle.
     */
    default Mono<String> critique(LoopContext context, java.util.List<String> revisitableRoles) {
        return Mono.just("CONTINUE");
    }

    /**
     * A stable, human-readable identity for whatever produced the output — e.g.
     * {@code claude-cli} or {@code http:gpt-4o}.
     *
     * <p>The loop deliberately does not know which model is behind this port, which is what
     * makes the model swappable. But a governed record must still answer "what wrote this?":
     * once models can be switched, an unattributed draft is an unanswerable question. So the
     * adapter names itself, and the loop records it against the draft.
     */
    default String identity() {
        return getClass().getSimpleName();
    }
}
