# MantleKeep — Python door client

Govern before execute, from Python. Submit an `Intent` to the one door, receive a rich
`Decision`, and let `GovernedWorker` own the decide-then-dispatch so an effect cannot run
outside governance.

This is one of several **peer clients** over the same `/api/govern` wire contract — Java, Python,
and a Rust client to come. None is derived from another; each is a thin client, idiomatic and
dependency-light in its own runtime, over the one contract the core defines. (The Go core is the
oracle today; a Rust core will take that role later, parity-gated.)

**No runtime dependencies.** This client speaks the door's HTTP contract with the standard library
alone (`urllib`, `json`, `dataclasses`) — the near-zero-dependency guarantee that matters most in
an air-gapped image.

## Install

```bash
pip install -e sdks/python        # from the repo, editable
# or add sdks/python to PYTHONPATH — it is pure stdlib, nothing to compile
```

Requires Python 3.9+.

## Use — govern an action

```python
from mantlekeep import DoorConfig, Intent, ServiceDoorClient

door = ServiceDoorClient(DoorConfig(base_url="http://door.internal:8080"))

decision = door.submit(Intent(
    action="launch.start",
    resource="launch/alice",
    goal="start a workload for alice",
    subject="alice",                 # travels as a header, never the body
    params={"image": "acme/tool:1.2"}, # values may be nested — the floor reads them
))

if decision.allowed:
    run_the_effect(decision.token)   # token = evidence of which decision authorised it
else:
    refuse(decision.denial_code, decision.reason)   # branch on the CODE, not the message
```

`Decision` carries the rich enterprise shape: `outcome` (`allow` | `deny` | `require_approval`),
`token`, `policy_id`, `expires_at`, typed `reasons` (`code` + `message`), and
`required_approvers`. **`require_approval` is not an allow** — `decision.allowed` is `False` for
it, so "not yet" can never be mistaken for a permit.

## Use — decide-then-dispatch, owned by the framework

```python
from mantlekeep import GovernedWorker, Intent

worker = GovernedWorker(door)

# The work runs ONLY on allow. A deny raises DecisionError (carrying the rich Decision)
# before the work is called — the effect and the decision cannot be separated.
result = worker.run(
    Intent(action="launch.start", resource="launch/alice",
           goal="start", subject="alice"),
    lambda token: start_workload(token),
)

# Saga steps under one prior approval — do not re-govern per step.
worker.run_under(approval_token, lambda token: next_step(token))
```

## Denial codes

Branch on `decision.denial_code` (the stable transport form of the engine's generic category):

| Code | Meaning |
|---|---|
| `DENY_FLOOR` | a policy floor blocked it (a cap, a pin, an admission rule, failsafe) |
| `DENY_SEPARATION_OF_DUTIES` | the actor may not also be the approver (includes an AI approving) |
| `DENY_IDENTITY` | the caller could not be resolved, or may not act for whom it claimed |
| `DENY_ACTION_NOT_ALLOWED` | no role or grant permits this action |
| `DENY_VALIDATION` | the request itself is malformed or incomplete |
| `DENY_POLICY_ERROR` | the engine could not reach a verdict |

## Delegation — a service acting for a person

Set `via` to the service and `subject` to the person. The person is recorded as the subject,
the service as `via`. The two identity headers are configurable and move together under a
rebrand — never one alone.

```python
Intent(action="launch.start", resource="launch/alice", goal="start",
       subject="alice", via="launcher-service")
```

## Pre-execution hook (any spawner, scheduler, or job runner)

`examples/govern_before_spawn.py` governs a launch through a "before it runs" callback: the door
decides before the executor runs, and a deny — or an unreachable door — aborts it. In an
air-gapped zone the `base_url` points at the door running **inside** the zone, which governs its
own launches offline.

## Tests

```bash
cd sdks/python && python3 -m unittest discover -s tests -t .
```

`test_decision_parse.py` pins the wire parser offline. `test_against_running_door.py` builds the
Go door, runs it, and drives allow / policy-deny / validation-deny through it — verifying the
client against the real contract, not a mock. It skips automatically if the Go toolchain is
absent.
