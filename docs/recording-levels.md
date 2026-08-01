# Recording levels — evidence is policy, the floor is not

**Decision (Stephen, co-owner, 2026-08-01):** how much a governed run records is the developer's design
choice, scaled to environment. Not every run must monitor and record everything — a dev sandbox should be
light; production should be full. Over-recording dev is cost with no auditor to read it.

**The line that does not move:** recording level controls what is *persisted*, never whether an action goes
*through the door*. This is the sealed-floor principle applied to observability — a config can choose the
policy; it can never reach the guarantee.

## Two axes, kept separate

| | Scales with environment? | Why |
|---|---|---|
| **Deciding** — govern-before-execute | **No.** Always on. | One door call; it is the floor. A dev action with a real effect still gets a decision. |
| **Recording** — saga records, evidence packs | **Yes.** Developer's choice. | Durable trails cost storage and noise; dev needs little, production needs all. |

## The levels

```
recording: none | decisions | steps | full
```

- **`none`** — a throwaway dev loop. The door still decides each governed action and that decision is on
  the chain (tiny, it is the floor's proof). No per-step trail is kept.
- **`decisions`** — the chain only. Who did what, allow/deny.
- **`steps`** — plus saga records: per-step status, compensation order, the tool's real output.
- **`full`** — plus the evidence pack: source snapshot, SBOM, artifacts. Production / regulated.

## The boundary a developer cannot cross

The recording level is the *only* knob. A developer designs their steps and how much is kept — they do not
get a raw runner. The governed saga runner still dispatches every step through `GovernedWorker`, so a dev
loop can be **silent but never ungoverned**. "Flexible saga worker" means flexible *content and verbosity*,
not a private execute loop — that private loop is exactly the bypass hole the framework closes.

## Where this lands in the build

- Saga records persist through `StorePort` (per-purpose bindable: chain and saga can use different backends
  by config), correlated to the chain head.
- The evidence pack is the build-domain feature (`EvidencePort`), production/regulated only — session and
  image produce no artifacts to pack, so they use `steps`, not `full`.
- `recording` is read as policy, so it is set per environment like any floor data — not compiled in.
