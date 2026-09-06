# mantlekeep-policy-postgres — governance policy in a database

This module holds the grant and floor documents in Postgres instead of in a file, and lets a
change to them go through the door like every other change. It implements the two ports the
core defines in `mantlekeep-control/grants`: **`Loader`**, so every replica reads the policy in
force from one place, and **`Writer`**, so `grants.Govern` can apply a change the door has
already allowed.

It is an **adapter**. It decides nothing. By the time `Write` runs, the door has allowed the
change; this module's job is to make the rows say what was approved, and to report what the
rows actually hold.

It is a **separate Go module** on purpose. The core links only bbolt; every adapter carries its
own client dependency, so a CVE — or a registry quarantine — in the Postgres tree can never
block the core's build. The dependency runs one way: `mantlekeep-policy-postgres →
mantlekeep-control`, never back.

---

## Which loader should this deployment use?

The file source (`grants.EnvSource`) is the **default, and it is the right default**. Choose
this one only when the deployment has a property the file source cannot serve.

| | File (`grants.EnvSource`) | Postgres (this module) |
|---|---|---|
| A policy change is… | a deploy | a governed write, live |
| Who writes it | nobody at runtime | the process handling an approved change |
| Time to take effect | a rollout | immediately, for every replica at once |
| "What did it say on the 14th?" | ask the git history of the file | ask the store |
| Review before it lands | a pull request | the door, plus whatever the floor requires |
| Failure mode to fear | a stale replica after a partial rollout | the database is down |
| Operational cost | none | a database that must be up for the door to decide |

**Choose the file source when** policy is a release artefact: it changes by pull request,
travels with the deploy, and is reviewed as code. This is most deployments, and its audit
story — a git history, signed commits, a review per change — is already excellent.

**Choose Postgres when** any of these is true:

- Policy is **edited through a UI**, by people who do not deploy. A grant must take effect
  without a rollout.
- Every replica must see a change at **the same moment**. A rollout makes some replicas serve
  the old policy for as long as it takes, and two replicas serving different policy is the
  divergence the revision exists to expose.
- An auditor asks **what the policy said on a date**, and "read the git log of a file, in a
  repo the auditor cannot see, and correlate it with a deploy timeline" is not an answer. This
  module keeps a row per change and can answer it in SQL.
- Policy and the hash chain need to be **restored together**. One database, one backup, one
  point-in-time recovery.

**Do not choose it** for the deny-all failure mode alone: this module treats an unreachable
database as an error, never as an empty policy, which means the door cannot decide while the
database is down. That is the correct behaviour — empty grants deny everything, so a silent
fallback would be a working deny-all that looked like a policy — but it does make the database
part of the control plane's availability. A deployment that cannot accept that should stay on
files.

---

## What it guarantees

**A revision is derived from CONTENT, never from a row id or a timestamp.** The revision comes
from `grants.RevisionOf` over the documents as loaded, exactly as the file source derives it. A
file deployment and this one, serving identical policy, report the identical sixteen
characters — which is what makes the migration below *provable* rather than merely intended.
The `revision` column exists for the operator reading `psql` and as the concurrency token; it
is never what `Load` returns.

**An unreadable database is an error, never an empty policy.** Every failure path returns an
error and no documents. A store that answered a failed read with an empty document would
produce a control plane that denies everything and looks, from outside, exactly like a
deliberate deny-all: nothing in the logs, nothing failing, just a system that has quietly
stopped letting anyone do anything.

**`Write` decides nothing.** No branch refuses a change on its merits. Granting what is already
granted, or revoking what was never held, is applied as the no-op it is and recorded with its
reason. A refusal invented in the writer would be a policy engine underneath the policy engine
— invisible to the floor that is supposed to govern it, and absent from the chain.

**A write is atomic, and two operators cannot lose each other's changes.** All three mechanisms
are used, and they answer different halves of the question:

- **A transaction spanning the whole read-modify-write.** This is the one that does the work.
  `Store.Update` reads the head under `SELECT … FOR UPDATE`, computes the change against it,
  and writes back — all inside one transaction, so there is no window in which another operator
  can commit between the read and the write, and therefore no race for anyone to lose.
- **Optimistic concurrency on the revision.** The `UPDATE` still carries `WHERE revision = …`.
  Under the lock it cannot fail; if it ever did, the head is not what the transaction read and
  committing would overwrite a change nobody saw. It is also the fallback contract for a store
  implementation that cannot hold a row — that one returns `ErrRevisionConflict`, having
  written nothing, and the caller re-reads and re-applies.
- **An append-only history.** Where one change supersedes another, both survive.

> This was not a design decided on paper. The first version computed the change *outside* the
> transaction and relied on optimistic retry alone. It never lost a change — but with sixteen
> operators writing at once, eight of them exhausted their retry budget and were told their
> approved change had failed. A governed change thrown away because somebody else was quicker
> teaches operators to retry by hand and eventually to edit the rows directly. The integration
> test in `sqlstore/` is what caught it; no fake could have.

**History is kept, deliberately.** A store that holds only current state can say what the
policy **is**. An audit asks what it **said** on the fourteenth — and the thing an auditor is
actually hunting is the permission that existed for three days and was quietly removed, which a
current-state-only store cannot show at all. One row per change buys that, and the rows carry
the full documents rather than a diff, so answering the question is a query and not a replay.

*Who* made a change is deliberately **not** in the history table. The actor, the decision, and
the before-revision are already on the hash chain, recorded by the door under `policy.grant`
before the row was written. A second, unsigned copy of "who did this" could disagree with the
signed one, and an audit trail with two answers is worse than one with a join. Join on
`revision`.

---

## Schema

The DDL ships as [`schema.sql`](schema.sql) and is applied by an operator, **not** generated at
runtime. A schema a process creates for itself is one nobody reviewed, that no migration tool
knows about, and that requires the application to hold DDL rights on its own policy store for
ever. It is also embedded as `pgpolicy.Schema` so a test can create its tables from the same
text an operator runs.

Two tables, both readable in `psql`:

```sql
-- what is in force right now
SELECT revision, updated_at, jsonb_pretty(grants_doc) FROM mantlekeep_policy_head;

-- every change ever applied, in order
SELECT applied_at, change_role, change_action, change_grant, change_reason
  FROM mantlekeep_policy_history ORDER BY seq;

-- what did the policy say on the 14th?
SELECT revision, jsonb_pretty(grants_doc) FROM mantlekeep_policy_history
 WHERE applied_at <= TIMESTAMPTZ '2026-08-14 23:59:59+00'
 ORDER BY seq DESC LIMIT 1;
```

Names are unqualified, so the deployment chooses the schema with `search_path` the same way its
migration tool does. Nothing is seeded: tables created with no policy row is an **error**
(`pgpolicy.ErrNoPolicy`), not an empty policy, because "schema applied, policy never loaded"
must not present as a working deny-all.

---

## Wiring it

```go
db, err := sqlstore.Open(ctx, os.Getenv("MANTLEKEEP_POLICY_DSN"))
if err != nil {
        return err // a policy store that cannot be reached fails at startup, not at the first decision
}
defer db.Close()

policy := pgpolicy.New(sqlstore.New(db))

// Read: hand it wherever a grants.Loader is wanted.
grantDocument, floorDocument, revision, err := policy.Load(ctx)

// Write: through the door, never around it.
after, err := grants.Govern(ctx, door, actor, policy, policy, grants.Change{
        Role:   "L2-Operator",
        Action: "deploy.dev",
        Grant:  true,
        Reason: "on call for the release window",
})
```

`Open` wires the pgx driver and returns a `*sql.DB` the **caller** owns and closes. A store that
owned its pool would own its credentials' lifetime too, and a deployment that brokers
credentials needs to hold that itself. A deployment with a pool already should skip `Open` and
call `sqlstore.New(db)`.

---

## Migrating between the two

The revision is what makes this safe in both directions: if the revision does not change, the
policy did not change, and that is a fact rather than a hope.

### File → Postgres

```go
// 1. Read the policy in force from the file source.
fileGrants, fileFloors, fileRevision, err := grants.EnvSource{}.Load(ctx)

// 2. Render it in the form the store holds, and see what revision it will serve.
snapshot, revision, err := pgpolicy.Encode(fileGrants, fileFloors)

// 3. Prove they are the same policy BEFORE anything is switched over.
if revision != fileRevision {
        return fmt.Errorf("migration would change the policy: %s becomes %s", fileRevision, revision)
}

// 4. Apply schema.sql through the deployment's migration tool, then seed.
created, err := store.Seed(ctx, snapshot, "migrated from MANTLEKEEP_POLICY_GRANTS on 2026-09-06")
```

`Seed` creates a policy where there is none and can never change one that already exists —
running it against a live store changes nothing and reports `false`. It is the one write in
this module that does not go through the door, because at the moment it runs there is no policy
for a door to consult, so it is confined to exactly that case. A bootstrap that could also
overwrite would be the easiest way in the whole system to grant yourself a role.

Then switch the deployment's `Loader` to `pgpolicy.New(store)` and confirm every replica reports
the revision you just proved.

**One difference to know about.** The file source *layers*: `MANTLEKEEP_PLATFORM_POLICY` and the
documents under `MANTLEKEEP_POLICY_DIR` merge into the base document, with the platform layer
sealing what a product may grant. This store holds the **resolved** result — one document set,
already merged. That is why step 1 loads through `grants.EnvSource` rather than reading the
files: what governs is what was merged, and it is the merged policy whose revision you compared.
A deployment that needs the layering to stay live at runtime should stay on the file source;
this module is for deployments whose layers are resolved at migration time and edited through
the door thereafter.

### Postgres → File

The bytes in the columns are valid policy documents:

```bash
psql -At -c "SELECT grants_doc FROM mantlekeep_policy_head" > grants.json
psql -At -c "SELECT floors_doc FROM mantlekeep_policy_head" > floors.json
psql -At -c "SELECT revision   FROM mantlekeep_policy_head"     # the revision to expect
```

Point `MANTLEKEEP_POLICY_GRANTS` and `MANTLEKEEP_POLICY_FLOORS` at them, load through
`grants.EnvSource`, and check the revision matches what the database reported. If it does, the
two deployments are serving the same policy. Leave the layering env vars unset — the exported
documents are already resolved.

---

## Testing

Every **rule** — what a revision is, how a change is applied, what happens when a read fails —
is decided in the parent package behind the `Store` interface, unit-tested with a fake, and
runs on any machine with no database:

```bash
go test ./...
```

The **SQL** that makes those rules true is proved against a real server, because a fake cannot
demonstrate that Postgres serialises two writers and assuming it is the mistake this module
must not make:

```bash
docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=policy postgres:17
MANTLEKEEP_POSTGRES_TEST_DSN='postgres://postgres:policy@127.0.0.1:5432/postgres?sslmode=disable' \
  go test -race -count=1 ./sqlstore/...
```

Unset, those tests skip. **The DSN must name a throwaway database** — the suite drops and
recreates the policy tables from `schema.sql`.

CI runs the first form. The second is what caught the retry-budget defect described above, and
is worth running before any change to `sqlstore/`.
