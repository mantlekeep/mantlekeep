# The door — as a library and over the wire

The **door** is the single choke point every action passes through. Submit an intent,
the door decides against policy, and — only if allowed — issues an execution token and
records the decision on the audit hash-chain. Govern first; execute second.

## As a Go library (embedded)

The core assembles a door from an identity resolver, a policy evaluator, and the audit
logger. The smoke command shows the whole flow end to end:

```bash
cd mantlekeep-control
go run ./cmd/mantlekeep
```

It submits a batch of intents, prints each verdict (allow / deny with a reason),
verifies `audit hash-chain intact: true`, then runs the orchestrator spine and
demonstrates a saga rollback with compensation.

The public seams (see `mantlekeep-control/app/door.go` and `contracts.go`):

- `Submitter.Submit(ctx, Intent) (ExecutionToken, error)` — govern an intent; a deny
  returns an error and no token, before any side effect.
- `PolicyEvaluator.Evaluate(ctx, PolicyInput) (Decision, error)` — the policy port. The
  default is a pure-Go RBAC resolver; swap it via `app.Options.Policy` (e.g. an OPA
  adapter) without the core ever linking that dependency.
- `AuditLogger` — the append-only, hash-chained ledger. Each record links to the prior
  record's hash, so tampering is detectable by re-verifying the chain.

The core embeds **no** policy by default (the "data wall"): actions, environments and
roles are supplied by product/team policy layers, cascaded default → platform → team,
honouring sealed floors. With no policy loaded, the door denies — safe by default.

## The wire contract (for out-of-process policy)

A policy plugin can live out of process — in WebAssembly, or as a service in a host's
own language. It receives a **frozen JSON shape**, `PolicyInput`:

```json
{
  "subject": { "id": "...", "roles": ["Operator"], "is_ai": false, "attrs": {"dept": "platform"} },
  "intent":  { "action": "job.promote", "resource": "...", "requester": "alice",
               "env": "PROD", "goal": "ship", "params": {"count": 1000} }
}
```

These dotted key paths are the contract. Renaming or removing a field is a **breaking
change** and must come with a MAJOR bump of the contract version — the shape is pinned
by a test (`mantlekeep-control/wireshape_test.go`) so a regression fails the build. A
policy returns a decision (allow/deny + reason); the door enforces it and records it.
