# mantlekeep-estate

Governs what runs where. A team declares the shape of its estate; this resolves that
declaration against a floor it cannot raise, decides where each piece belongs, submits every
change to the door, and reports what is actually out there against what was approved.

It carries **no third-party dependencies**. The only thing it requires is the core.

## What it does

```
manifest ──▶ floor ──▶ placement ──▶ door ──▶ adapter ──▶ backend
             │          │             │                    │
             │          │             │                    └─ observe ──▶ drift
             │          │             └─ allow · deny · require approval
             │          └─ which cluster, and why
             └─ limits a request cannot raise
```

- **Floor** — resolved LAST, so a request can never reach past it. An environment the floor
  has not ruled on is refused rather than guessed.
- **Placement** — a team declares intent (environment, purpose, data residency), not a
  cluster. Residency is checked first: it is the one constraint whose breach cannot be undone.
- **Gate** — a change's consequence decides whether it needs a second person. Config can
  tighten a gate; nothing can loosen one.
- **Drift** — what exists but was never approved, and what exists but not as approved, with
  the backend's own record of which writer set which field.

## Writing an adapter

Implement `Approved` and wrap it with `Guarded`, which runs the refusals every adapter owes:

```go
type Approved interface {
    Asset() string
    Kinds() []string
    Observe(ctx context.Context, team string) (Observed, error)
    ApplyApproved(ctx context.Context, token mantlekeep.ExecutionToken, change DesiredItem) error
}

func New(...) estate.Port { return estate.Guarded(&Adapter{...}) }
```

You implement `ApplyApproved`, never `Apply` — so an empty token, an expired token, or a
change belonging to another asset cannot reach your backend. There is no code path that
arrives without those checks.

Authorise with `token.Value`; record with `token.IntentID`. **Never write `Value` anywhere an
object, a log or a manifest can carry it** — it is a live capability until it expires.

## Watching before changing

`ReadOnly` wraps an adapter so `Apply` refuses rather than acts. Observation still works, so
an estate can be watched — declared versus observed, drift, and workloads nobody approved —
before anything is allowed to change it. The writing adapter is simply not registered, so
there is no branch to get wrong.

## Composition

Adapters are passed IN; this module imports none of them. That is what keeps a backend's
dependency tail out of the engine — a consumer who needs no Kubernetes links none of it.

## Status

Early. The contracts are settled and exercised end to end against a real cluster; the
operational edges are not finished. Known limits are recorded in the source rather than
implied: observation is per-kind, in-memory stores are named as such, and reconcile runs when
asked rather than on a timer.
