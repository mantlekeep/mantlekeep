package dev.mantlekeep.door;

import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.spi.WorkerPort;
import java.util.Objects;

/**
 * Execution with the door in front of it, owned by the framework rather than by each
 * product.
 *
 * <p>The problem this exists to remove: if a product holds both a {@link DoorClient} and a
 * {@link WorkerPort}, governance is something the product remembers to do. It can call the
 * worker directly — not maliciously, usually, but because a new endpoint took a shortcut
 * and nobody noticed in review. Governance that depends on remembering is governance that
 * eventually is not done.
 *
 * <p>So the framework hands a product THIS, and does not hand it the raw port. The
 * sequence — decide, then dispatch only on allow — is {@code final} here, in the layer the
 * product does not write. A product supplies WHAT to run (its adapter); the framework owns
 * WHEN it may run.
 *
 * <p>This is the template-method rule applied to execution: a sealed skeleton with an open
 * hook. The hook is the adapter; the skeleton is not overridable.
 *
 * <h2>What this does and does not guarantee</h2>
 *
 * <p><b>Does:</b> the governed path is the only path the product is <em>given</em>. A
 * bypass now requires deliberately constructing the raw adapter rather than merely
 * forgetting a call, which is the difference between an accident and a decision — and a
 * decision leaves a diff a reviewer can see.
 *
 * <p><b>Does not:</b> stop code that constructs its own adapter anyway. Both live in one
 * process, and no in-process arrangement survives an author who does not want it to. For a
 * control that removes the capability rather than raising its cost, the executor must hold
 * no credentials until the door releases them — see {@code docs/credential-brokering.md}.
 * Stating the limit is part of the design, not a caveat on it.
 */
public final class GovernedWorker {

    private final DoorClient doorClient;
    private final WorkerPort delegate;

    /**
     * @param doorClient the door every dispatch is decided by
     * @param delegate   the product's adapter — what actually performs the effect
     */
    public GovernedWorker(DoorClient doorClient, WorkerPort delegate) {
        this.doorClient = Objects.requireNonNull(doorClient, "doorClient");
        this.delegate = Objects.requireNonNull(delegate, "delegate");
    }

    /**
     * Submits the intent, and dispatches to the adapter only if the door allowed it.
     *
     * <p>A deny returns without touching the adapter, so no side effect occurs. The
     * decision — allow or deny — is on the audit chain either way, because the door
     * recorded it before returning.
     *
     * @param intent          what is being asked, and by whom
     * @param workRequestJson the work for the adapter, per the door's versioned contract
     * @return the adapter's receipt on allow
     * @throws DoorDeniedException when the door refuses; the effect never runs
     */
    public String run(Intent intent, String workRequestJson) {
        Decision decision = doorClient.decide(intent);
        if (!decision.allowed()) {
            // Deliberately thrown rather than returned: a caller that ignores a return
            // value would proceed as though nothing had happened, and a refusal that can
            // be ignored is not a refusal.
            throw new DoorDeniedException(intent, decision);
        }
        return delegate.dispatch(workRequestJson);
    }
}
