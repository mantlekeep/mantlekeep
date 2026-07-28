# Why govern human + AI actions

A short, honest case for adopting MantleKeep — what problem it solves, and how.

---

## The problem

Software systems now take consequential actions on two tracks at once: **humans**
click deploy, approve merges, cut releases; and increasingly **AI agents** open
merge requests, bump dependencies, run flows, and touch production. Most of that
happens with no single, enforced gate:

- **No approval gate before the effect.** The check (if any) lives inside the code
  path that also does the work, or in a CI script someone can edit, or nowhere. Nothing
  structurally stops an action from running before it is allowed.
- **No trustworthy audit.** Logs are written by the same systems taking the actions,
  are editable, and rarely capture *what was attempted and denied* — only what
  succeeded. When an auditor asks "who approved this, and on what basis?", the answer is
  reconstructed after the fact, not recorded at the moment of decision.
- **AI with no boundary it cannot cross.** An agent that can act can usually also
  approve — its own pull request, its own change, its own deployment. "The AI reviewed
  it" is not review. Nothing enforces that the actor and the approver are different.
- **Policy scattered and drifting.** The same rule ("promotes to PROD need a second
  person") is re-implemented per service, per flow, per team, and slowly disagrees
  with itself.

None of these are exotic. They are the default state of a system that grew action by
action without a governance layer.

---

## MantleKeep's answer

MantleKeep adds one thing: a **governance layer** every action passes through, human
or AI, *before* it runs.

- **One door — govern before you execute.** Every action is submitted to a single
  choke point that decides against policy and issues an execution token *before* any
  side effect happens. A deny aborts before anything runs. There is no bypass: in the
  Java SDK, an intent that the door denies throws, and the effect after that line simply
  never executes.

- **A tamper-evident hash-chain.** Every decision — allow **and** deny — is appended to
  an append-only ledger, each record linked to the previous record's hash. Altering a
  past record breaks the chain, and re-verification detects it. Evidence is a byproduct
  of governing, not a separate logging effort you have to remember. A deny is evidence
  too: the trail records what was *attempted*, not only what succeeded.

- **The sealed floor: AI cannot approve its own work.** Some guarantees are not policy
  you can configure away — they are structural. An AI agent is a first-class subject at
  the door, but it is *propose-only*: it can open a draft, request, or proposal, and it
  is denied the approve/merge path. A configuration can tighten a rule; it can never
  reach past the floor to let the actor approve itself.

- **Policy in one place, as data.** Rules are supplied to the generic engine as layered
  data (default → platform → team), most-specific-wins, with sealed keys a lower layer
  may only tighten. The engine that enforces them is the same everywhere, so the rule
  stops drifting between services.

You can watch all of this happen in about five minutes:
[build-your-first-product.md](build-your-first-product.md) is a runnable worked example
where an allowed action reaches the worker, a denied one never does, and every verdict
lands on a chain that verifies intact.

---

## What MantleKeep does *not* claim

Honesty is part of the pitch:

- The chain proves a **governed step occurred, in order, by whom** — it does **not**
  prove the step did what its stated goal described. Governance records intent and
  decision; it is not a correctness proof of the work.
- MantleKeep is a **governance** framework, not a runtime one. It does not replace your
  orchestrator, your CI, or your model. It rides them and governs the actions they
  take — you plug your backends in behind ports (see
  [extending.md](extending.md)).
- It is **deny-by-default**. With no policy loaded, nothing is allowed. That is a
  feature, but it means adoption starts by declaring what *is* permitted.

---

## Who it's for

- Teams running **AI agents against real systems** who need a boundary the agent cannot
  cross and a record of everything it attempted.
- **Regulated or audited environments** that must show, on demand, who did what, who
  approved it, and on what basis — with evidence captured at decision time, not
  reconstructed later.
- **Sovereign / air-gapped** deployments: the door and its chain can run entirely
  in-process per zone, governing offline, with no dependency on an external service.
- Anyone consolidating **scattered approval logic** into one enforced, uniform gate for
  both humans and machines.

If your actions — human or AI — currently run without a single gate in front of them
and without a record you could hand an auditor, that is the gap MantleKeep fills.

---

Continue with [architecture.md](architecture.md) for how it fits together, or
[build-your-first-product.md](build-your-first-product.md) to build one.
