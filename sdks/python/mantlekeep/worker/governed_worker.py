"""GovernedWorker — the framework owns decide-then-dispatch, so an effect cannot run
outside governance by accident.

A product does not call the door and then, separately, run the work; that split is exactly
where a step slips past the door. Here the sequence is owned: ``run`` governs first and
executes the work only on allow. The raw effect is a callable passed IN, not an object the
product holds, so there is no ungoverned executor lying around to call directly.
"""

from __future__ import annotations

from typing import Callable, Optional, TypeVar

from ..door.errors import DecisionError
from ..model.decision import Decision
from ..model.intent import Intent

T = TypeVar("T")


class DoorPort:
    """The one method GovernedWorker needs from a door client: submit an intent.

    Named as a port so a test can pass a fake and production passes ServiceDoorClient. Any
    object with ``submit(Intent) -> Decision`` satisfies it (structural).
    """

    def submit(self, intent: Intent) -> Decision:  # pragma: no cover - interface
        raise NotImplementedError


class GovernedWorker:
    """Runs work only after the door allows it."""

    def __init__(self, door: DoorPort):
        self._door = door

    def run(self, intent: Intent, work: Callable[[Optional[str]], T]) -> T:
        """Govern the intent, then execute ``work`` only on allow.

        ``work`` receives the execution token (evidence of which decision authorised it).
        A deny or require_approval raises DecisionError carrying the rich Decision — the
        work never runs. This is the whole point: the decision precedes the effect, and a
        refusal aborts before any side effect.
        """
        decision = self._door.submit(intent)
        if not decision.allowed:
            raise DecisionError(decision)
        return work(decision.token)

    def run_under(self, approval_token: str, work: Callable[[str], T]) -> T:
        """Execute a saga step under a single prior approval, without re-governing per step.

        A run approved as a unit should not re-ask the door for every step (that would turn
        transition-level governance into per-step governance). The caller holds the token
        from an earlier ``run`` and executes subsequent steps under it. Governance of the
        transition already happened; this carries its authorisation forward.
        """
        return work(approval_token)
