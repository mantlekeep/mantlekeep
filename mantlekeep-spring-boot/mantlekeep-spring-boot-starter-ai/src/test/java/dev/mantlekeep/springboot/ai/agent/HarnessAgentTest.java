package dev.mantlekeep.springboot.ai.agent;

import static org.assertj.core.api.Assertions.assertThat;

import dev.mantlekeep.springboot.agent.LoopContext;
import dev.mantlekeep.springboot.agent.Role;
import java.util.List;
import org.junit.jupiter.api.Test;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

/**
 * Unit tests for {@link HarnessAgent} — driven entirely through a stub
 * {@link ClaudeRunner}, so no real {@code claude} process is launched.
 */
class HarnessAgentTest {

    private static final Role ROLE = new Role("spec", "write the spec");
    private static final LoopContext CONTEXT = new LoopContext("ship the loop", List.of("prior"));

    @Test
    void draftReturnsRunnerOutput() {
        HarnessAgent agent = new HarnessAgent(prompt -> Mono.just("drafted spec"));

        StepVerifier.create(agent.draft(ROLE, CONTEXT))
                .expectNext("drafted spec")
                .verifyComplete();
    }

    @Test
    void draftStreamAdaptsTheDraftIntoAStream() {
        HarnessAgent agent = new HarnessAgent(prompt -> Mono.just("streamed spec"));

        StepVerifier.create(agent.draftStream(ROLE, CONTEXT))
                .expectNext("streamed spec")
                .verifyComplete();
    }

    @Test
    void propagatesRunnerError() {
        RuntimeException boom = new IllegalStateException("claude CLI exited 1");
        HarnessAgent agent = new HarnessAgent(prompt -> Mono.error(boom));

        StepVerifier.create(agent.draft(ROLE, CONTEXT))
                .expectErrorMatches(t -> t == boom)
                .verify();
    }

    @Test
    void passesABuiltPromptToTheRunner() {
        StringBuilder seen = new StringBuilder();
        HarnessAgent agent = new HarnessAgent(prompt -> {
            seen.append(prompt);
            return Mono.just("ok");
        });

        agent.draft(ROLE, CONTEXT).block();

        assertThat(seen.toString())
                .contains("spec")            // role name
                .contains("write the spec")  // role description
                .contains("ship the loop")   // goal
                .contains("prior");          // prior output
    }

    @Test
    void buildPromptOmitsEmptySections() {
        String prompt = HarnessAgent.buildPrompt(
                new Role("review", ""), new LoopContext("", List.of()));

        assertThat(prompt)
                .contains("review")
                .doesNotContain("Loop goal:")
                .doesNotContain("completed so far");
    }
}
