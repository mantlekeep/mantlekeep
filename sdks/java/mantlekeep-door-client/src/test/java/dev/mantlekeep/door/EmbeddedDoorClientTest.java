package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.ExecutionToken;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.door.model.Subject;
import java.util.List;
import org.junit.jupiter.api.Test;

class EmbeddedDoorClientTest {

    @Test
    void allowReturnsTokenAndLandsOnChain() {
        try (EmbeddedDoorClient doorClient =
                new EmbeddedDoorClient(new InMemoryNativeCore(List.of()))) {
            Intent intent = Intent.of("job.promote", "ship release 1.2")
                    .asSubject(Subject.ofId("lead-bob"));

            ExecutionToken token = doorClient.submit(intent);

            assertFalse(token.value().isBlank(), "allow must carry an execution token");
            List<AuditRecord> chain = doorClient.audit();
            assertEquals(1, chain.size());
            assertEquals("job.promote", chain.get(0).action());
            assertEquals("lead-bob", chain.get(0).subjectId());
            assertEquals("allow", chain.get(0).decision());
            assertTrue(doorClient.verify());
        }
    }

    @Test
    void denyThrowsBeforeAnyEffectAndIsStillRecorded() {
        try (EmbeddedDoorClient doorClient =
                new EmbeddedDoorClient(new InMemoryNativeCore(List.of("service.deploy")))) {
            Intent denied = Intent.of("service.deploy", "deploy the service");

            DoorDeniedException denial =
                    assertThrows(DoorDeniedException.class, () -> doorClient.submit(denied));

            assertTrue(denial.getMessage().contains("service.deploy"));
            assertTrue(denial.reason().contains("denied by test policy"),
                    "the door's reason must survive into the exception");
            assertEquals("deny", doorClient.audit().get(0).decision(),
                    "a deny is evidence too — it must land on the chain");
        }
    }

    @Test
    void decideIsTheNonThrowingView() {
        try (EmbeddedDoorClient doorClient =
                new EmbeddedDoorClient(new InMemoryNativeCore(List.of("service.deploy")))) {
            Decision denial = doorClient.decide(Intent.of("service.deploy", "try it"));
            assertFalse(denial.allowed());
            assertFalse(denial.reason().isBlank());
        }
    }

    @Test
    void closeReleasesTheCore() {
        InMemoryNativeCore core = new InMemoryNativeCore(List.of());
        new EmbeddedDoorClient(core).close();
        assertTrue(core.isClosed());
    }
}
