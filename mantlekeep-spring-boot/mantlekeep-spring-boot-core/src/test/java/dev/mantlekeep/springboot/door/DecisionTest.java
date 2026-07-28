package dev.mantlekeep.springboot.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

class DecisionTest {

    @Test
    void failsClosedWhenOutcomeIsNull() {
        Decision d = new Decision(null, null, null, null, null, null);
        assertFalse(d.allowed());
        assertEquals(Decision.Outcome.DENY, d.outcome());
        assertTrue(d.reasons().isEmpty());
        assertTrue(d.requiredApprovers().isEmpty());
    }

    @Test
    void allowedOnlyWhenOutcomeIsAllow() {
        Decision allow = new Decision(Decision.Outcome.ALLOW, "tok", null, "p1", null, null);
        assertTrue(allow.allowed());

        Decision review = new Decision(Decision.Outcome.REQUIRE_APPROVAL, "", null, "p2", null, null);
        assertFalse(review.allowed());
    }
}
