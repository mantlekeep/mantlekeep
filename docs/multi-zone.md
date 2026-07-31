# Multiple zones — federated doors, per-zone chains, data residency

**The situation.** A pipeline spans places that are legally or physically separated: different
jurisdictions, an isolated network, a partner's estate. Records from one may not lawfully leave it, and one
of them may have no route to the others at all.

**The answer in one line:** each zone has **its own door, its own chain, and its own store**; a step that
crosses a boundary is governed **twice — never zero times**; and what moves between zones is a **hash, not
a record**.

---

## 1. The door is federated, not multiplied

Read this carefully, because it is the point at which the model could be misread as weakened:

> **One door per governance domain — and every action passes through the door of the domain it acts in.**

A second door is **not** an alternative route around the first. A run in Zone A that dispatches work into
Zone B produces **two** decisions:

1. In **Zone A**: *may this run dispatch work into Zone B at all?* — recorded on chain A.
2. In **Zone B**: *may this work execute here?* — recorded on chain B, judged by **Zone B's** policy.

**Crossing a boundary means more governance, not less.** Zone B is not obliged to trust Zone A's verdict;
it applies its own floors, and it may refuse work that Zone A approved. That is the property a regulator
cares about — a stricter jurisdiction cannot be overridden from a looser one.

If a design ever lets a step execute in a zone *without* that zone's door deciding, it is not federation;
it is a bypass.

## 2. A cross-zone step in the DAG

A step is dispatched through the `WorkerPort` like any other; the only difference is that the worker on the
far side is in another zone and governs locally before it runs.

```
   ZONE A                                  ZONE B
   ┌────────────────────────┐              ┌────────────────────────┐
   │ run.execute      →  ✔  │              │                        │
   │ step.dispatch    →  ✔  │──────────────▶ step.execute    →  ✔   │
   │   (to zone-b)          │   request     │   judged by B's floors│
   │                        │◀──────────────│                        │
   │ step.completed         │  {status,     │                        │
   │   result: sha256:…     │   result-hash}│                        │
   │        chain-A         │              │        chain-B         │
   └────────────────────────┘              └────────────────────────┘
```

**What chain A records:** that a step was dispatched to Zone B, and the *hash* of its outcome.
**What chain B records:** the execution itself — who, what, which policy, what the tool actually did.

The detail stays where it was created. Chain A can prove *that* something happened in B and that its result
has not changed since; it cannot reveal *what* — which is exactly the property residency requires.

**If Zone B denies:** Zone A's run fails with B's reason, and both chains hold their half of the story.
A deny in B is a normal, recorded outcome — not a transport error.

## 3. Different stores per zone — by construction

The audit store is a **port**, so each zone selects its own adapter by configuration:

| Zone | Store | Why |
|---|---|---|
| primary region | Postgres | replicas behind a load balancer, shared chain |
| second region | whatever that estate mandates | the core never learns which |
| isolated site | the embedded file store | no database server, no reachback, still governed |

The core links no database driver; each adapter is its own module. A zone inherits only the driver it chose.

## 4. Hash compatibility is a cross-jurisdiction contract

Every adapter **must** compute record hashes identically. With one store this is tidiness; across zones it
is load-bearing:

- a chain from a Postgres zone and one from an embedded zone must be **comparably verifiable**;
- a result hash produced in Zone B must mean the same thing when checked in Zone A;
- an exported chain must verify against a live one.

**Test it directly:** append identical records through every adapter and assert identical hashes. Without
that test, a divergence is discovered during an audit, which is the worst possible moment.

## 5. Assurance across zones without moving data

An auditor covering several jurisdictions needs to know each chain is intact — not to read the records.
So publish the **proof**, not the content:

```json
{ "chain_id": "zone-b", "head_hash": "…", "count": 41208,
  "verified_at": "2026-07-30T09:00:00Z", "signature": "…" }
```

A chain head hash is a digest, not a record: it demonstrates integrity and length while revealing nothing
about who did what. Each zone publishes its attestation to a central register on its own schedule; an
isolated zone can export the same document by hand, since it is a few hundred bytes.

**What this gives you:** group-level assurance — *every zone's ledger is intact, here is the proof* —
with **zero cross-border data transfer**.

**Caveat, stated honestly:** a digest derived from personal data can still be regulated where it permits
re-identification. A head hash over an aggregate chain is a weak candidate for that; a per-subject hash is
a stronger one. Confirm with counsel before relying on it — this is a design that *supports* residency, not
a legal opinion that satisfies it.

## 6. What a zone must be able to do alone

A zone is only genuinely isolated if it can, with no reachback:

- **decide** — its policy is local, so a deny needs nobody's permission;
- **record** — its chain is local and append-only;
- **verify** — it can re-walk its own chain and prove it intact;
- **refuse** — it can decline work dispatched from elsewhere.

If any of those requires another zone to be reachable, the zone is not isolated — it is merely far away,
and it will fail closed the moment the link does.

## 7. Acceptance

1. A cross-zone step produces **two** decisions, one on each chain.
2. Zone B **denying** work that Zone A allowed stops the step, and both chains record their half.
3. Zone A's chain contains **no** record content from Zone B — only a dispatch record and a result hash.
4. Zones running **different store adapters** produce identical hashes for identical records.
5. A zone with its link severed still decides, records, and verifies.
6. An attestation from each zone verifies against that zone's chain, and reveals no record content.
