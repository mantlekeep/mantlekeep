"""Unit tests for the wire parser — no door required, so the contract is pinned even offline."""

import unittest

from mantlekeep import Decision
from mantlekeep.model.decision import (
    OUTCOME_ALLOW,
    OUTCOME_DENY,
    OUTCOME_REQUIRE_APPROVAL,
)


class DecisionParseTest(unittest.TestCase):
    def test_allow_carries_the_enterprise_fields(self):
        decision = Decision.from_wire(
            {
                "outcome": "allow",
                "token": "abc123",
                "intentId": "INT-1",
                "policyId": "rbac",
                "expiresAt": "2026-08-01T12:00:00Z",
                "reasons": [],
            }
        )
        self.assertTrue(decision.allowed)
        self.assertEqual(decision.token, "abc123")
        self.assertEqual(decision.policy_id, "rbac")
        self.assertEqual(decision.expires_at, "2026-08-01T12:00:00Z")
        self.assertIsNone(decision.denial_code)

    def test_deny_is_typed_and_not_allowed(self):
        decision = Decision.from_wire(
            {
                "outcome": "deny",
                "intentId": "INT-2",
                "policyId": "rbac",
                "reasons": [
                    {"code": "DENY_ACTION_NOT_ALLOWED", "message": "no role permits action job.run"}
                ],
            }
        )
        self.assertFalse(decision.allowed)
        self.assertEqual(decision.outcome, OUTCOME_DENY)
        self.assertEqual(decision.denial_code, "DENY_ACTION_NOT_ALLOWED")
        self.assertIn("no role permits", decision.reason)
        self.assertIsNone(decision.token)

    def test_require_approval_is_not_an_allow(self):
        decision = Decision.from_wire(
            {
                "outcome": "require_approval",
                "intentId": "INT-3",
                "policyId": "rbac",
                "reasons": [{"code": "DENY_SEPARATION_OF_DUTIES", "message": "needs a second party"}],
                "requiredApprovers": ["L1-Architect"],
            }
        )
        # require_approval must NOT read as allowed — it is 'not yet', and treating it as a
        # permit is exactly the bug the rich shape exists to prevent.
        self.assertFalse(decision.allowed)
        self.assertEqual(decision.outcome, OUTCOME_REQUIRE_APPROVAL)
        self.assertEqual(decision.required_approvers, ["L1-Architect"])

    def test_a_missing_reasons_key_is_tolerated(self):
        decision = Decision.from_wire({"outcome": OUTCOME_ALLOW, "token": "t"})
        self.assertTrue(decision.allowed)
        self.assertEqual(decision.reasons, [])


if __name__ == "__main__":
    unittest.main()
