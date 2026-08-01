"""The Intent — WHAT is being requested, declared before it is executed.

Mandatory on every submission: a request with no declared goal is refused by the door
(declare-before-execute). This mirrors the Java ``Intent`` and the Go ``mantlekeep.Intent``
so a Python caller submits the same shape every other client does.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Mapping, Optional


@dataclass(frozen=True)
class Intent:
    """A single governed request submitted through the one door.

    Attributes:
        action: the action name, e.g. ``"session.spawn"``. Generic to the engine — it
            names no product; the meaning lives in the policy data a product supplies.
        goal: the human declaration of intent. Required — the door denies an empty goal.
        resource: the scope target the action applies to, e.g. ``"project/demo"``.
        params: open parameters the policy floor reads (a cap, an environment, a scope).
            Values may be nested (a dict, a list, a number) — not strings only. The Java
            SDK once capped this to string→string and a nested ``capped_map`` floor became
            unreachable; this type does not repeat that.
        subject: who is acting. In service mode this travels as a HEADER, never the body
            (a body-supplied caller would be forgeable), so it is optional here and the
            client sets the header from it.
        via: the application asserting the subject, when a service acts for a person. Both
            are recorded: the person as subject, the service as via.
    """

    action: str
    goal: str
    resource: str = ""
    params: Mapping[str, Any] = field(default_factory=dict)
    subject: Optional[str] = None
    via: Optional[str] = None
