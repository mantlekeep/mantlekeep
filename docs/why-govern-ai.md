# Why govern human + AI actions

What MantleKeep does and when to use it.

---

## What it addresses

Both humans and AI agents take consequential actions against real systems — deploys,
merges, releases, dependency bumps, production changes. MantleKeep provides a single
enforced gate in front of those actions, for the cases where you need:

- **An approval gate before the effect**, separate from the code path that does the work.
- **A trustworthy audit** that records what was attempted and denied, not only what
  succeeded.
- **A boundary an AI agent cannot cross** — enforcing that the actor and the approver
  are different.
- **Policy in one place** rather than re-implemented per service, flow, or team.

---

## How it works

MantleKeep adds a **governance layer** every action passes through, human or AI,
*before* it runs.

- **One door — govern before you execute.** Every action is submitted to a single gate
  that decides against policy and issues an execution token *before* any side effect
  happens. A deny aborts before anything runs. In the Java SDK, an intent that the door
  denies throws, and the effect after that line never executes.

- **A tamper-evident hash-chain.** Every decision — allow **and** deny — is appended to
  an append-only ledger, each record linked to the previous record's hash. Altering a
  past record breaks the chain, and re-verification detects it. A deny is recorded too:
  the trail captures what was *attempted*, not only what succeeded.

- **The sealed floor: AI cannot approve its own work.** An AI agent is a first-class
  subject at the door, but it is *propose-only*: it can open a draft, request, or
  proposal, and it is denied the approve/merge path. A configuration can tighten a rule
  but cannot loosen past the floor to let the actor approve itself.

- **Policy as data.** Rules are supplied to the generic engine as layered data
  (default → platform → team), most-specific-wins, with sealed keys a lower layer may
  only tighten. The engine that enforces them is the same everywhere.

[build-your-first-product.md](build-your-first-product.md) is a runnable worked example
where an allowed action reaches the worker, a denied one never does, and every verdict
lands on a chain that verifies intact.

---

## Scope and limits

- The chain proves a **governed step occurred, in order, by whom** — it does **not**
  prove the step did what its stated goal described. Governance records intent and
  decision; it is not a correctness proof of the work.
- MantleKeep is a **governance** framework, not a runtime one. It does not replace your
  orchestrator, your CI, or your model. It rides them and governs the actions they
  take — you plug your backends in behind ports (see
  [extending.md](extending.md)).
- It is **deny-by-default**. With no policy loaded, nothing is allowed, so adoption
  starts by declaring what *is* permitted.

---

## Who it's for

- Teams running **AI agents against real systems** who need a boundary the agent cannot
  cross and a record of everything it attempted.
- **Regulated or audited environments** that must show, on demand, who did what, who
  approved it, and on what basis — with evidence captured at decision time.
- **Sovereign / air-gapped** deployments: the door and its chain can run entirely
  in-process per zone, governing offline, with no dependency on an external service.
- Anyone consolidating **scattered approval logic** into one enforced, uniform gate for
  both humans and machines.

---

Continue with [architecture.md](architecture.md) for how it fits together, or
[build-your-first-product.md](build-your-first-product.md) to build one.
