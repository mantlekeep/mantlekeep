# Build your first governed product (~5 minutes)

This tutorial builds the smallest possible **governed product** on MantleKeep: a
worker that runs sessions, but only when the **door** allows it. You will assemble
the governance loop from the SDK's **ports** and two ready-made example adapters,
write one adapter of your own (a `WorkerPort`), submit a few intents, and watch the
one rule the whole framework serves in action:

> **Govern before you execute.** Every action is decided against policy and recorded
> on a tamper-evident hash-chain *before* any side effect runs. A deny aborts before
> anything happens.

You need a **JDK 25** toolchain. Nothing else — no Docker, no database, no server.

Everything here is real, runnable code. The complete program is at
[`examples/FirstProduct.java`](examples/FirstProduct.java); the snippets below are
excerpts from it.

---

## The shape of a governed product

A governed host never runs work inline. It **decides** through the door, then
**delegates** the effect to a port:

```
intent ──▶ [ 1. GOVERN ]  ask the PolicyEvaluator port: allow or deny?
           [ 2. RECORD ]  append the verdict to the hash-chained audit store
           [ 3. EXECUTE ] only on ALLOW, dispatch to the WorkerPort
```

You will use three ports from `dev.mantlekeep.spi`:

| Port | What it decides / does | Adapter you'll use |
|------|------------------------|--------------------|
| `PolicyEvaluator` | may this subject do this action? | `AllowlistPolicyEvaluator` (example) |
| `StorePort` | the append-only audit chain | `InMemoryStore` (example) |
| `WorkerPort` | run the approved unit of work | **you write this one** |

The two example adapters ship in the repo. Swapping either for a real backend (OPA
for policy, Postgres for the store) is a **config change, not a rewrite** — that is
the whole point of the ports-and-adapters seam. See [extending.md](extending.md).

---

## Step 1 — build the SDK (gets you the example adapters)

From the repo root:

```bash
cd sdks/java
./gradlew build
```

Expected: `BUILD SUCCESSFUL`. This produces the SPI jar and the two example adapter
jars under each module's `build/libs/`.

---

## Step 2 — write your `WorkerPort` adapter

The core knows only the **port**; your backend knowledge lives in the adapter. A real
one emits a Kubernetes Job, triggers Jenkins, or runs a pod. This minimal one returns
a dispatch receipt — enough to prove the door only ever calls it *after* an allow.

```java
static final class SessionWorker implements WorkerPort {
    @Override
    public String dispatch(String workRequestJson) {
        System.out.println("    [worker] EXECUTED " + workRequestJson);
        return "{\"receipt\":\"session-0001\",\"status\":\"started\"}";
    }
}
```

## Step 3 — assemble the governance loop

The door governs first, records every verdict (allow **and** deny — a deny is
evidence too), and dispatches to the worker only on allow. Each audit record links to
the previous record's hash, so tampering is detectable:

```java
String submit(String subjectId, String action, String goal, Map<String, String> attributes) {
    // GOVERN FIRST — ask the policy port before doing anything.
    PolicyVerdict verdict = policy.evaluate(subjectId, action, attributes);

    // RECORD — link each record to the prior record's hash (tamper-evident).
    String decision = verdict.allowed() ? "allow" : "deny";
    String record = "{...,\"decision\":\"" + decision + "\",\"prev\":\"" + previousHash + "\"}";
    chain.append(record);
    previousHash = sha256(previousHash + record);

    if (!verdict.allowed()) {
        // DENY ABORTS — the worker is never reached, no side effect runs.
        return null;
    }
    // ALLOW — and only now does the effect run, through the worker port.
    return worker.dispatch("{\"action\":\"" + action + "\",\"goal\":\"" + goal + "\"}");
}
```

## Step 4 — wire it and submit intents

Wire the door from the ports and adapters, then submit. The policy allows exactly two
actions; everything else is denied **by default**:

```java
PolicyEvaluator policy = new AllowlistPolicyEvaluator(Set.of("session.start", "session.stop"));
StorePort chain = new InMemoryStore();
WorkerPort worker = new SessionWorker();
Door door = new Door(policy, chain, worker);

door.submit("dev-alice", "session.start",   "open a work session",  Map.of("env", "DEV"));   // allowed
door.submit("dev-alice", "session.stop",    "close the session",    Map.of("env", "DEV"));   // allowed
door.submit("ci-agent",  "session.approve", "approve my own session", Map.of("env", "PROD")); // DENIED
```

---

## Step 5 — run it

The example adapters are on the classpath as the jars you built in Step 1. From the
repo root, this single command compiles and runs the program (the classpath is built
from the jar locations, so it survives version bumps):

```bash
CP="$(find sdks/java/mantlekeep-adapter-spi/build/libs \
           sdks/java/mantlekeep-adapter-policy-allowlist/build/libs \
           sdks/java/mantlekeep-adapter-store-inmemory/build/libs -name '*.jar' | tr '\n' ':')"

javac -cp "$CP" -d /tmp/mk-first docs/examples/FirstProduct.java
java  -cp "/tmp/mk-first:$CP" FirstProduct
```

**Expected output** (verified — this is the real run):

```
MantleKeep — first governed product
──────────────────────────────────────────
  ALLOW dev-alice → session.start
    [worker] EXECUTED {"action":"session.start","goal":"open a work session"}
  ALLOW dev-alice → session.stop
    [worker] EXECUTED {"action":"session.stop","goal":"close the session"}
  DENY  ci-agent → session.approve  (action 'session.approve' is not on the allowlist [session.start, session.stop])
──────────────────────────────────────────
audit hash-chain (3 records, oldest first):
  1. {"subject":"dev-alice","action":"session.start","decision":"allow","reason":"","prev":"GENESIS"}
  2. {"subject":"dev-alice","action":"session.stop","decision":"allow","reason":"","prev":"bb381a01f4ec7ea241dc031d4aa796d4f90c6655b95d638a1203f109e0ce9ba8"}
  3. {"subject":"ci-agent","action":"session.approve","decision":"deny","reason":"action 'session.approve' is not on the allowlist [session.start, session.stop]","prev":"6bcd9d0aab93c197b1ba235734818e57a27dc0870d6b588ca9e564ae7dff746f"}
chain intact: true
```

## What you just proved

- **Govern before execute.** The two allowed actions reached the worker
  (`[worker] EXECUTED …`); the denied one **never did**. The worker is structurally
  unreachable without an allow.
- **Deny-by-default.** `session.approve` was not on the allowlist, so it was denied
  with the reason spelled out — a bare "no" is not governable evidence.
- **A deny is evidence too.** All three verdicts — including the deny — are on the
  chain. The audit trail is the record of what was *attempted*, not just what ran.
- **Tamper-evident.** Each record carries the hash of the previous one
  (`prev`), and `chain intact: true` re-walked and confirmed the links. Change any
  earlier record and the walk breaks.

---

## Where to go next

- **Swap a backend by config.** Replace `AllowlistPolicyEvaluator` with an OPA
  adapter, or `InMemoryStore` with Postgres — without touching the loop. The pattern
  (SPI + `ServiceLoader` discovery + config selection) is in
  [extending.md](extending.md).
- **Govern a Spring method with one annotation.** In a Spring Boot service, annotate
  a method `@MantlekeepIntent(action = "session.start")` and the aspect submits it
  through the door before the body runs — no loop code. See
  [sdk-quickstart.md](sdk-quickstart.md).
- **Understand the whole picture.** [architecture.md](architecture.md) and
  [why-govern-ai.md](why-govern-ai.md).

> **From this toy to production:** here you assembled the loop by hand to *see* every
> step. In a real product you don't — the SDK's `DoorClient` is that loop, wired from
> config alone (`mantlekeep.door.*`), pointed at the real MantleKeep core (as a remote
> service, or embedded in-process). Your `WorkerPort` adapter and your policy choice
> carry over unchanged.
