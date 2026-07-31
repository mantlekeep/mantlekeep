package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.ExecutionToken;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.door.model.Subject;
import dev.mantlekeep.spi.WorkerPort;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/**
 * The point of {@link GovernedWorker} is a negative: on a deny, the adapter is not
 * reached. So the adapter here records whether it ran, and the tests assert on that
 * rather than on a return value — "no side effect" is the guarantee, and a guarantee
 * about absence has to be tested by observing absence.
 */
class GovernedWorkerTest {

    /** Records whether it was reached, so a test can assert it was NOT. */
    private static final class RecordingWorker implements WorkerPort {
        private boolean dispatched;

        @Override
        public String dispatch(String workRequestJson) {
            dispatched = true;
            return "{\"receipt\":\"ok\"}";
        }
    }

    /** A door with a fixed verdict — the decision is the input under test. */
    private static DoorClient doorThat(boolean allowed, String reason) {
        return new DoorClient() {
            @Override
            public Decision decide(Intent intent) {
                return allowed ? Decision.allow("token-123") : Decision.deny(reason);
            }

            @Override
            public List<AuditRecord> audit() {
                return List.of();
            }

            @Override
            public boolean verify() {
                return true;
            }

            @Override
            public void close() {}
        };
    }

    private static Intent intent() {
        return new Intent("", new Subject("lead-bob", "operator"), "session.deploy",
                "session-a", "deploy a governed session", Map.of());
    }

    @Test
    void anAllowedIntentReachesTheAdapter() {
        RecordingWorker worker = new RecordingWorker();

        String receipt = new GovernedWorker(doorThat(true, ""), worker).run(intent(), "{}");

        assertTrue(worker.dispatched, "an allowed intent must reach the adapter");
        assertEquals("{\"receipt\":\"ok\"}", receipt);
    }

    @Test
    void aDeniedIntentNeverReachesTheAdapter() {
        RecordingWorker worker = new RecordingWorker();
        GovernedWorker governed = new GovernedWorker(doorThat(false, "resource exceeds its cap"), worker);

        assertThrows(DoorDeniedException.class, () -> governed.run(intent(), "{}"));

        assertTrue(!worker.dispatched,
                "THE guarantee: a denied intent must produce NO side effect. If the adapter "
                        + "ran, governance is advice rather than control.");
    }

    @Test
    void workUnderAnExistingApprovalDoesNotAskTheDoorAgain() {
        RecordingWorker worker = new RecordingWorker();
        // A door that would DENY — proving runUnder does not consult it. A saga approved
        // as a unit must not re-ask per step, or transition-level governance silently
        // becomes phase-level and the chain fills with decisions nobody made.
        GovernedWorker governed = new GovernedWorker(doorThat(false, "would deny"), worker);

        governed.runUnder(new ExecutionToken("token-from-the-approval"), "{}");

        assertTrue(worker.dispatched, "an already-approved run must be able to execute its steps");
    }

    @Test
    void dispatchingWithoutAnApprovalTokenIsRefused() {
        GovernedWorker governed = new GovernedWorker(doorThat(true, ""), new RecordingWorker());

        // Not a security control — it is traceability. Work that names no decision cannot
        // be tied back to an approval, which is the point of carrying the token at all.
        assertThrows(IllegalArgumentException.class,
                () -> governed.runUnder(new ExecutionToken(""), "{}"));
    }

    @Test
    void theRefusalCarriesTheDoorsReasonRatherThanAGenericFailure() {
        GovernedWorker governed = new GovernedWorker(
                doorThat(false, "resource exceeds its cap (cpu=16 exceeds cap 8)"), new RecordingWorker());

        DoorDeniedException denied =
                assertThrows(DoorDeniedException.class, () -> governed.run(intent(), "{}"));

        // A refusal a caller cannot explain gets reported to a user as "something went
        // wrong", which teaches exactly the wrong lesson about what the door does.
        assertTrue(denied.getMessage().contains("resource exceeds its cap"),
                "the door's reason must survive to the caller, got: " + denied.getMessage());
    }
}
