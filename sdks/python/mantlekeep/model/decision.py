"""The Decision — the door's verdict, in the rich 3-state enterprise shape.

A flat ``{allowed, reason}`` cannot answer what an auditor asks: who must approve, under
which policy, why, and until when. This mirrors the Go ``mantlekeep.Decision`` and the Java
``Decision`` record exactly, parsing the canonical ``/api/govern`` response.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, List, Mapping, Optional

OUTCOME_ALLOW = "allow"
OUTCOME_DENY = "deny"
OUTCOME_REQUIRE_APPROVAL = "require_approval"


@dataclass(frozen=True)
class Reason:
    """One typed reason on the wire: a stable code plus the human message.

    ``code`` is the transport form of the engine's generic denial category
    (``DENY_FLOOR``, ``DENY_SEPARATION_OF_DUTIES``, ``DENY_IDENTITY``,
    ``DENY_ACTION_NOT_ALLOWED``, ``DENY_VALIDATION``, ``DENY_POLICY_ERROR``). Branch on
    ``code``, never on ``message`` — the message is for humans and may be reworded.
    """

    code: str
    message: str


@dataclass(frozen=True)
class Decision:
    """The door's verdict on an intent.

    Attributes:
        outcome: ``allow`` | ``deny`` | ``require_approval``.
        token: the execution token, present only on allow. Evidence of which decision
            authorised the work — opaque and unsigned, not a key.
        intent_id: the id the door assigned this submission.
        policy_id: which policy produced the verdict (for audit).
        expires_at: when an allow's authorisation lapses (RFC3339), else None.
        reasons: typed reasons; empty on a clean allow.
        required_approvers: for require_approval, the roles a second party would need.
    """

    outcome: str
    token: Optional[str] = None
    intent_id: str = ""
    policy_id: str = ""
    expires_at: Optional[str] = None
    reasons: List[Reason] = field(default_factory=list)
    required_approvers: List[str] = field(default_factory=list)

    # ── Convenience read-side, so simple callers need not inspect the full shape ──

    @property
    def allowed(self) -> bool:
        """True only for a clean allow. require_approval is NOT an allow — it is 'not yet'."""
        return self.outcome == OUTCOME_ALLOW

    @property
    def denial_code(self) -> Optional[str]:
        """The stable code of the first reason, or None. Branch on this, not the message."""
        return self.reasons[0].code if self.reasons else None

    @property
    def reason(self) -> Optional[str]:
        """The first human message, or None."""
        return self.reasons[0].message if self.reasons else None

    @staticmethod
    def from_wire(payload: Mapping[str, Any]) -> "Decision":
        """Parse a ``/api/govern`` response body into a Decision.

        Tolerant of a missing ``reasons`` (older/allow shapes) and of either the rich
        ``requiredApprovers`` or its absence, so one parser reads every outcome.
        """
        raw_reasons = payload.get("reasons") or []
        reasons = [
            Reason(code=str(item.get("code", "")), message=str(item.get("message", "")))
            for item in raw_reasons
        ]
        return Decision(
            outcome=str(payload.get("outcome", "")),
            token=payload.get("token") or None,
            intent_id=str(payload.get("intentId", "")),
            policy_id=str(payload.get("policyId", "")),
            expires_at=payload.get("expiresAt") or None,
            reasons=reasons,
            required_approvers=list(payload.get("requiredApprovers") or []),
        )
