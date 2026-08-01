"""Errors the door client raises."""

from __future__ import annotations

from ..model.decision import Decision


class DecisionError(Exception):
    """Raised when a governed action was not allowed.

    Carries the full rich Decision so a caller that catches it keeps the policy id, the
    typed reason code, and (for require_approval) who may sign off — the same fields the
    wire surfaces. A caller that only logs the exception still gets a sensible message.
    """

    def __init__(self, decision: Decision):
        self.decision = decision
        code = decision.denial_code or decision.outcome
        message = decision.reason or "the door did not allow this action"
        super().__init__(f"{code}: {message}")


class DoorUnavailableError(Exception):
    """Raised when the door could not be reached or answered with a non-JSON/transport error.

    Distinct from DecisionError: a deny is an ANSWER (the door decided no), while this is
    the absence of an answer (network, timeout, malformed transport). A caller must not
    treat an unreachable door as a permit.
    """
