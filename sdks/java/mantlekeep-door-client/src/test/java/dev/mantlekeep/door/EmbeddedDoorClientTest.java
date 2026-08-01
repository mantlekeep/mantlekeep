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
    void embeddedDecodesTheRichNativeContract() {
        try (EmbeddedDoorClient doorClient =
                new EmbeddedDoorClient(new InMemoryNativeCore(List.of("service.deploy")))) {
            Decision allow = doorClient.decide(Intent.of("job.promote", "ship it"));
            assertEquals(Decision.Outcome.ALLOW, allow.outcome());
            assertEquals("policy.test.allowlist", allow.policyId());
            assertEquals("2026-08-01T00:00:00Z", allow.expiresAt());

            Decision deny = doorClient.decide(Intent.of("service.deploy", "deploy"));
            assertEquals(Decision.Outcome.DENY, deny.outcome());
            assertEquals("policy.test.allowlist", deny.policyId());
            assertEquals("DENY_ACTION_NOT_ALLOWED", deny.reasons().get(0).code());
        }
    }

    @Test
    void embeddedDecodesRequireApprovalFromTheNativeContract() {
        // A native core that returns the third outcome — proving the embedded path is not
        // a boolean in disguise: it decodes who-must-approve off the FFI boundary too.
        NativeCore reviewCore = new NativeCore() {
            @Override
            public String submitJson(String intentJson) {
                return "{\"outcome\":\"require_approval\",\"policy_id\":\"policy.test.allowlist\","
                        + "\"required_approvers\":[\"L4-Approver\",\"security-officer\"],"
                        + "\"reasons\":[{\"code\":\"REQUIRE_APPROVAL\",\"message\":\"needs a second signer\"}]}";
            }

            @Override
            public String auditJson() {
                return "[]";
            }

            @Override
            public boolean verifyChain() {
                return true;
            }

            @Override
            public void close() {
            }
        };
        try (EmbeddedDoorClient doorClient = new EmbeddedDoorClient(reviewCore)) {
            Decision decision = doorClient.decide(Intent.of("release.cut", "cut 1.2"));
            assertEquals(Decision.Outcome.REQUIRE_APPROVAL, decision.outcome());
            assertFalse(decision.allowed());
            assertEquals(List.of("L4-Approver", "security-officer"), decision.requiredApprovers());
        }
    }

    @Test
    void closeReleasesTheCore() {
        InMemoryNativeCore core = new InMemoryNativeCore(List.of());
        new EmbeddedDoorClient(core).close();
        assertTrue(core.isClosed());
    }
}
