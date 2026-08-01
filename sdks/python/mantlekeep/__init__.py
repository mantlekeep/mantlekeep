"""MantleKeep door client for Python — govern before execute, through one door.

One of several peer clients over the same ``/api/govern`` wire contract (Java and Python today,
a Rust client to follow); each is thin and idiomatic in its own runtime, and none is derived
from another. Submit an :class:`Intent` to the door, receive a rich :class:`Decision`, and let
:class:`GovernedWorker` own the decide-then-dispatch so an effect cannot run outside governance.
Standard library only — no runtime dependencies.
"""

from .door.config import DoorConfig
from .door.errors import DecisionError, DoorUnavailableError
from .door.service_door_client import ServiceDoorClient
from .model.decision import (
    OUTCOME_ALLOW,
    OUTCOME_DENY,
    OUTCOME_REQUIRE_APPROVAL,
    Decision,
    Reason,
)
from .model.intent import Intent
from .worker.governed_worker import DoorPort, GovernedWorker

__all__ = [
    "DoorConfig",
    "DecisionError",
    "DoorUnavailableError",
    "ServiceDoorClient",
    "Decision",
    "Reason",
    "OUTCOME_ALLOW",
    "OUTCOME_DENY",
    "OUTCOME_REQUIRE_APPROVAL",
    "Intent",
    "DoorPort",
    "GovernedWorker",
]
