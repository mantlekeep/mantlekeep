# A shared audit chain — the Postgres adapter (design)

**Why.** The default chain is bbolt: a single-process embedded file. Two processes cannot open it for
writing, so today the door **cannot be replicated** — not because replication is unwise, but because the
store forbids it. This adapter is the one change that unlocks both high availability and horizontal reads.

**The constraint that shapes everything.** The chain is *inherently sequential*: each record's hash covers
the previous record's hash. Two writers that both read `prev_hash = X` produce a **fork** — two
plausible-looking chains — which destroys the tamper-evidence the chain exists to provide. So any design
must serialise the append. There is no lock-free version of this that keeps the guarantee.

---

## 1. Split the two halves first

Governance decomposes into parts with opposite scaling properties, and the whole design follows from it:

| | Nature | Scaling |
|---|---|---|
| **Decide** — evaluate policy for a subject + intent | a pure function | stateless; scale replicas freely, no coordination |
| **Record** — append to the chain | inherently sequential | must serialise; one writer at a time per chain |

So: replicate the deciders, serialise only the append. The append is a hash and an insert — microseconds of
serialised work per governed action.

**Sanity check before building anything:** governed actions are *human-scale* — deploys, approvals,
promotions. Tens per minute, not thousands per second. A single Postgres writer clears that by orders of
magnitude. **The real driver for this work is availability, not throughput** — do not design for a load
that will not arrive.

## 2. Module boundary (get this right or it undoes a core guarantee)

**The adapter does NOT go in `mantlekeep-control`.** The core links almost nothing today, and that is a
feature: a host adopting it inherits no database driver, and no driver CVE. Put the adapter in its **own
module** with its own `go.mod`:

```
mantlekeep-audit-postgres/        ← its own module; depends on the core, never the reverse
  go.mod                          ← the pgx driver lives HERE
  postgres.go                     ← implements mantlekeep.AuditLogger
  schema.sql
```

A deployment that wants Postgres imports the adapter and wires it in; a deployment that does not, links no
driver at all. Dependency direction stays one-way: **adapter → core**.

## 3. Schema

```sql
CREATE TABLE audit_record (
  seq         BIGSERIAL PRIMARY KEY,
  chain_id    TEXT        NOT NULL,   -- the governance domain (see §6)
  ts          TIMESTAMPTZ NOT NULL,
  intent_id   TEXT        NOT NULL,
  subject_id  TEXT        NOT NULL,
  action      TEXT        NOT NULL,
  decision    TEXT        NOT NULL,
  policy_id   TEXT        NOT NULL,
  is_ai       BOOLEAN     NOT NULL,
  via         TEXT        NOT NULL DEFAULT '',
  prev_hash   TEXT        NOT NULL,
  hash        TEXT        NOT NULL,
  UNIQUE (chain_id, hash)
);

CREATE INDEX audit_record_chain_seq ON audit_record (chain_id, seq DESC);

-- Append-only, enforced by the DATABASE rather than by convention.
REVOKE UPDATE, DELETE, TRUNCATE ON audit_record FROM <app_role>;
```

The revoke matters more than it looks: a ledger whose append-only-ness depends on the application
remembering not to issue an `UPDATE` is a ledger with an undocumented trust assumption. Let the database
refuse.

## 4. The append

```sql
BEGIN;
  -- Serialise writers for THIS chain only. Transaction-scoped: released on commit or
  -- rollback, with no lock table to maintain and no contention between chains.
  SELECT pg_advisory_xact_lock(hashtext($chain_id));

  SELECT hash FROM audit_record
   WHERE chain_id = $chain_id
   ORDER BY seq DESC LIMIT 1;          -- the tail; empty ⇒ genesis, prev_hash = ''

  -- compute hash in Go, using the SAME hashRecord as the embedded store (§5)

  INSERT INTO audit_record (...) VALUES (...);
COMMIT;
```

Why an advisory lock rather than `SELECT … FOR UPDATE` on the tail row: there is no tail row to lock on an
empty chain, and locking a moving target invites subtle races. An advisory lock keyed by `chain_id` is
explicit about what is being serialised — one chain — and lets independent chains proceed in parallel.

**On failure, roll back the whole transaction.** A partially applied append is a broken chain, which is
indistinguishable from tampering.

## 5. The hash function is a compatibility contract

The adapter **must** compute hashes with the same function as the embedded store. If the two differ:

- a chain cannot be migrated between stores;
- `Verify` gives different answers for the same history;
- an auditor comparing an exported chain against a live one sees a mismatch that means nothing.

Export the hashing from the core as a small public function and have both stores call it. **Add a test
that appends the same records to both stores and asserts identical hashes** — that test is what keeps the
two implementations honest as either changes.

## 6. `chain_id` is the sharding seam — and it should be the governance domain

Do not shard by load. Shard by **governance domain**, which for an air-gapped topology means **one chain
per zone**. This is not a compromise: a zone is the boundary an auditor reasons about, so a per-zone chain
is the *correct* unit, independently verifiable, and it keeps an isolated zone genuinely self-sufficient —
it governs and records with no reachback.

Sharding by load, by contrast, splits one governance domain across chains for a reason that means nothing
to an auditor, and leaves you reconciling trails that should never have been separated.

> **Across zones, see `multi-zone.md`.** Data residency makes a per-zone chain and store mandatory rather
> than optional, and turns the hash-compatibility requirement in §5 into a cross-jurisdiction contract.

## 7. Verify

Walk a chain in `seq` order for a `chain_id`, recompute each record's hash from its content plus the
previous hash, and compare. Report the **first** divergence with its `seq` — "the chain is broken" is much
less useful than "the chain is broken at record 412", which localises the tampering.

For long chains, support verifying a range so an operator can bisect rather than re-walk millions of rows.

## 8. Deployment shape

```
        load balancer
              │
   ┌──────────┼──────────┐      each replica: a STATELESS decider
 door-1     door-2     door-3   (policy evaluation needs no coordination)
   └──────────┼──────────┘
        Postgres — the chain    (the append is serialised per chain_id)
```

Replicas are interchangeable and hold no chain state. Losing one loses nothing.

## 9. Acceptance

1. **Concurrency:** N writers appending simultaneously produce **one** chain with no gaps, no duplicate
   `prev_hash`, and `Verify` intact. Run it with real concurrency, not sequentially.
2. **Hash compatibility:** the same records appended to the embedded store and to Postgres produce
   **identical** hashes.
3. **Append-only is enforced by the database:** an `UPDATE` or `DELETE` as the app role is refused.
4. **Tamper detection:** modify one row as a superuser; `Verify` reports the correct `seq`.
5. **Crash safety:** kill a writer mid-append; the chain is either unchanged or contains a complete record
   — never a partial one.
6. **Chain isolation:** appends to different `chain_id`s do not block each other.
7. **The core still links no database driver** — check the core module's dependency graph, not just that
   it compiles.
