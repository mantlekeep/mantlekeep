"""Govern a launch through the MantleKeep door before an executor runs it — a worked example.

Many systems expose a "before it runs" callback: a scheduler, a spawner, a job runner. Wiring
that callback to the door makes the launch a GOVERNED action — the door decides first, and a
deny (or an unreachable door) aborts before anything starts. Govern-before-execute.

This is deliberately generic: ``launch.start`` is an example action, ``acme`` is a placeholder
brand, and ``launcher`` is any object with a name, a target, and an environment map. Adapt the
attribute names to whichever system you are integrating.
"""

from mantlekeep import DoorConfig, Intent, ServiceDoorClient
from mantlekeep.door.errors import DecisionError, DoorUnavailableError

# One door client per process. In an air-gapped zone this base_url points at the door running
# INSIDE the zone — the zone governs its own launches, offline.
_door = ServiceDoorClient(
    DoorConfig(
        base_url="http://door.acme.svc:8080",
        # Rename this header for a branded deployment; the door must trust the same name.
        caller_header="X-Acme-User",
    )
)


def govern_before_start(launcher):
    """Refuse a launch unless the door allows it.

    Raising aborts the launch, so a deny — or an unreachable door — stops it from starting.
    An unreachable door is never treated as a permit.

    ``launcher`` is any object exposing:
      - ``requester``   : who is asking (becomes the subject)
      - ``target``      : what is being launched (becomes the resource)
      - ``environment`` : a mutable dict the caller can annotate before the run
      - optional resource caps the policy floor may read
    """
    requester = launcher.requester

    intent = Intent(
        action="launch.start",
        resource=f"launch/{launcher.target}",
        goal=f"start {launcher.target} for {requester}",
        subject=requester,
        params={
            "target": launcher.target,
            # Resource caps the door's floor can read (a capped_map floor, for instance).
            "cpu_limit": getattr(launcher, "cpu_limit", None),
            "mem_limit": getattr(launcher, "mem_limit", None),
        },
    )

    try:
        decision = _door.submit(intent)
    except DoorUnavailableError as unavailable:
        # Fail closed: no answer from the door is not permission to launch.
        raise RuntimeError(f"launch refused — governance door unavailable: {unavailable}")

    if not decision.allowed:
        raise RuntimeError(
            f"launch refused by policy [{decision.denial_code}]: {decision.reason}"
        )

    # Allowed. The token is evidence of which decision authorised this launch; carry it on the
    # run so the started workload can be tied back to the decision on the chain.
    launcher.environment["MANTLEKEEP_APPROVAL_TOKEN"] = decision.token or ""
    return None
