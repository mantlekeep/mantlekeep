"""Door client configuration.

The header NAMES are configurable, not constants, so a branded deployment can rename them
without a code change — and the two identity headers move together (renaming one and not
the other silently dropped delegation in an earlier Java client; this keeps the pair).
"""

from __future__ import annotations

from dataclasses import dataclass

# The framework defaults. A branded product overrides these via config; nothing downstream
# hardcodes them. Kept in step with the Java client's defaults so every SDK agrees on the wire.
DEFAULT_CALLER_HEADER = "X-Mantlekeep-User"
DEFAULT_ON_BEHALF_OF_HEADER = "X-Mantlekeep-On-Behalf-Of"


@dataclass(frozen=True)
class DoorConfig:
    """How to reach the door and how to name the caller on the wire.

    Attributes:
        base_url: the door's base URL, e.g. ``"http://door.internal:8080"``.
        caller_header: the header carrying the acting subject. Must match the header the
            door server trusts (its TrustedUserHeader).
        on_behalf_of_header: the header by which a service names the person it acts for.
            Renamed together with caller_header under a rebrand — never one alone.
        timeout_seconds: how long to wait for the door before treating it as unavailable.
            An unreachable door is never a permit.
    """

    base_url: str
    caller_header: str = DEFAULT_CALLER_HEADER
    on_behalf_of_header: str = DEFAULT_ON_BEHALF_OF_HEADER
    timeout_seconds: float = 10.0
