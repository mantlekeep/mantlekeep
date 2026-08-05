# Templates, behaviours and workers

**Read this with `layering.md`.** That doc covers what each layer owns; this one covers how a pipeline is
assembled. A pipeline is a **template** instantiated with a **behaviour**, whose steps are executed by
**workers**.

The building blocks are standard: template-method, factory-selected strategies, workers executing phases,
and dependency graphs run in topological order. Two things are added on top — **every phase passes through
the door**, and step logic runs in a **sandbox** rather than trusted on the build agent. The concepts map
one to one onto a conventional CI system, which makes it a practical migration target.

```
manifest ──▶ template + behaviour ──▶ composed DAG ──▶ each step through the DOOR
                                                          └─▶ worker executes ──▶ hash-chain
```

---

## 1. The four concepts

| Concept | What it is | Who owns it |
|---|---|---|
| **Manifest** | the per-repository input: what to build, the chains, the values | the repository |
| **Template** | the reusable pipeline *shape* + its config DTO | generic / domain layer |
| **Behaviour** | the *lifecycle*: which phases run, in what order, with what semantics | selected per style; overridden per team |
| **Worker** | executes one phase against real tooling | generic, with per-style implementations |

How they fit together: the manifest selects a behaviour → the behaviour orchestrates workers → each phase is
submitted to the door before it runs.

The concepts are kept separate because a lifecycle is not universal. Building a service, applying
infrastructure-as-code, and migrating a database have different phases, parameters, and release
restrictions, so the framework does not hardcode one lifecycle.

## 2. Behaviour selection is driven by repository style

Different repositories want different pipeline shapes:

| Style | Shape |
|---|---|
| `single-project` | one project per repository — a single linear lifecycle |
| `multi-project-independent` | many projects, each built independently |
| `multi-project-linked` | many projects with declared dependencies — a DAG, run in parallel levels |

The manifest declares the style; a **factory** selects the matching behaviour family, so the framework does
not impose one lifecycle on every repository.

## 3. The template-method rule

A behaviour is a **sealed algorithm with open steps**:

```java
public abstract class PipelineBehaviour {

    /** FINAL — the algorithm. A team cannot reorder or skip governance. */
    public final RunOutcome run(PipelineContext context) {
        for (Phase phase : phases()) {                 // the lifecycle
            doorClient.submit(intentFor(phase, context));   // GOVERN, then execute
            execute(phase, context);
        }
        return outcome(context);
    }

    /** The lifecycle. A behaviour family defines its own phases. */
    protected abstract List<Phase> phases();

    /** OPEN — the hooks a team overrides. Override ONE; inherit the rest. */
    protected void packageArtifact(PipelineContext context) { /* sensible default */ }
    protected void containerise(PipelineContext context)    { /* sensible default */ }
    protected void verify(PipelineContext context)          { /* sensible default */ }
}
```

Two properties follow:

- **The skeleton is sealed; the steps are open.** `run()` is `final`, so a team cannot reorder phases, skip
  the door, or drop a gate. Everything a team changes is a `protected` hook.
- **Override the methods that differ.** A team subclass changes those and inherits the rest:

```java
public final class TeamBehaviour extends DomainBehaviour {
    @Override protected void verify(PipelineContext context) { /* the team's own check */ }
    // packaging, containerising, phases — all inherited
}
```

If a team is overriding more than two hooks, that is a signal the base is missing something — fix the base.

## 4. Config DTOs inherit too

The type system mirrors the config cascade: a template/config DTO extends a base; a team **adds fields** and
does not restate the base.

```java
public class TemplateConfig      { /* generic fields */ }
public class DomainConfig  extends TemplateConfig { /* domain additions */ }
public class TeamConfig    extends DomainConfig   { /* team additions only */ }
```

A team DTO that redeclares a base field lets values drift, with no clear precedence — avoid it.

## 5. Packages state the layer

One direction, never reaching up:

```
com.acme.sdlc          ← generic (the framework you ship)
com.acme.domain.sdlc   ← domain layer (shared across teams)
com.acme.team.sdlc     ← a specific team
```

Group id names the layer's owner; the package path states the layer. The base is what you ship and version;
the overrides belong to the team. No flat default package.

## 6. Chains become a DAG

For the linked style the manifest declares chains and dependencies
(a `dependsOn` list per project). Normalise them into a subproject DAG and run
**topological levels in parallel**, preserving the manifest's order within a level.

**Fail fast, loudly:** an unknown chain, a dependency cycle, or an ambiguous target must abort before any
step runs, rather than silently dropping a subproject.

## 6b. The execution unit — govern per phase, execute per GROUP

Governing every phase separately does not require running every phase separately. Running each phase in its
own pod means a pipeline spends most of its time waiting for schedulers and image pulls.

**Two boundaries, and they are orthogonal:**

| | Granularity | Cost |
|---|---|---|
| **Governance boundary** — what needs a decision | **per phase**, always | one call to the door: milliseconds |
| **Execution boundary** — what shares a process, filesystem, workspace | **per group**, configurable | a new unit: scheduling + image pull + start, often tens of seconds |

Governing finely is cheap; executing finely is expensive. The two are configured independently.

### Three modes; the middle one is the default

```
unit = run          one execution unit for the whole run; phases share a workspace.
                    Fastest. The conventional-agent model. Weakest isolation.

unit = branch       one unit per INDEPENDENT branch of the DAG.        ← default
                    Independent branches run in parallel; phases within
                    a branch run sequentially and share a workspace.

unit = phase        one unit per phase. Strongest isolation, slowest;
                    nothing is shared, so every phase re-fetches its inputs.
```

`branch` is the default because it matches what the DAG already declares: branches with no dependency on each
other need not share a workspace and can run at the same time. Phases that do depend on each other pass work
through the filesystem, so keeping them in one unit avoids staging artefacts between pods.

This matches how mature Kubernetes-native CI systems group a task's steps into a single pod as sequential
containers over a shared workspace, and use separate pods for independent tasks.

### The mode never changes whether something is governed

**Every phase submits to the door in every mode.** The mode decides *where* work runs, not *whether* it was
decided. Reducing the number of execution units must not reduce the number of decisions on the chain; if a
chain shows fewer records after a performance change, that is a bug. Assert it in the test suite.

### Isolation is a security decision, not only a performance one

Phases sharing a unit share a filesystem. That is fine for phases that trust each other — a build feeding a
verify. It is **not** fine for a phase that executes untrusted code, which must either get its own unit or
run inside the sandbox.

That is what the sandbox is for (§8): a tool from a registry is untrusted regardless of the pod it sits in,
so sandboxing lets untrusted work stay in a shared, fast unit instead of paying pod isolation for every
step.

### If a unit is still slow to start

Reduce the start-up cost rather than the granularity, once the granularity is right:

- keep a **warm pool** of pre-scheduled idle units, so a run claims one instead of waiting for a scheduler;
- keep unit images **small and pre-pulled** on the nodes that will run them;
- leave **scheduling headroom** so a unit is not waiting on a node that is itself being provisioned.

These do not help if the pipeline is still spawning a unit per phase — set the granularity first, then the
warm start.

## 6c. Saga boundaries

A flow often crosses services: one saves a session, another publishes the artifact. The question is whether
that is **one saga spanning both** or **two sagas that hand off**.

**Two, because of compensation.** If the first service's saga included the second's work, then on a later
failure it would have to *undo* that work — delete from a registry it has no authority over. That would
require giving it an undo hook into the other service's domain. So **each service owns the saga for what it
can undo**:

```
  SERVICE A                                SERVICE B
  ── saga 1, owned here ──                 ── saga 2, owned here ──
  DOOR: a.action  ✔                        DOOR: b.action  ✔
    step        ↺ compensable                step        ↺ compensable
    step        ↺ compensable                step        ↺ compensable
    hand off ─────────────────────────▶      (begins)
  (ends here)
```

**Two decisions, not three.** The first service's decision covers its own action *including the handoff* —
"may this be done, and sent there". The second judges on **its own** floors and may refuse what the first
requested. Crossing a boundary means more governance, not less.

**Link the halves with a self-expiring artifact, not a distributed transaction.** If the second service
never acts, the artifact expires on its own, which removes the need for cross-service compensation — the
orphan cleans itself up rather than requiring a coordinator with reach into both.

| What fails | What happens |
|---|---|
| the first decision is denied | nothing runs; one deny recorded |
| a step in saga 1 fails | saga 1 compensates its own steps; service B never involved |
| the second decision is denied | the artifact already exists and **expires by TTL**; both halves recorded |
| a step in saga 2 fails | saga 2 compensates its own steps; service A unaffected |

**Prefer an asynchronous handoff.** Awaiting the second service's result invites the first to "handle" the
second's failures, which erodes the boundary. Drop the artifact; let the other side pick it up and govern
it.

## 7. Platform versus content

The two parts change at different rates and are governed differently:

| | Changes | Governed by |
|---|---|---|
| **PLATFORM** — the engine, the door, the sealed floors | rarely | a change request, like any core system |
| **CONTENT** — a template, a behaviour, a tool | often | the **door**: approval + hash-chain record |

A content change is a **versioned artifact**, approved at the door, and that approval record is the change
evidence — no fresh change request per behaviour change.

Practical consequence: **templates, behaviours and tools are versioned artifacts, pinned by version** — not
edited in place. A team upgrades by moving to a new version, and the move is recorded.

## 8. Where the door and the sandbox enter

Two things this model adds over a conventional CI system:

1. **Governance granularity — decide this per deployment; both answers are defensible.**

   | | What reaches the chain | Suits |
   |---|---|---|
   | **Transition-level** *(the usual first slice)* | propose · approve · reject — two or three records per run | one approval authorises the whole run; no floor needs step-specific data |
   | **Phase-level** *(the fuller form)* | every phase, each with its own parameters | a floor must gate one phase differently from another — "deploy to prod" separately from "build" |

   **Start at transition level.** It is simpler and enough when the approval covers the run. Move to phase
   level when a floor needs to inspect a *particular* phase's parameters.

   The trade is real in both directions: transition-level means an approved run executes its phases
   unchecked, so the approval must be worth that. Phase-level means more records, and records nobody reads
   dilute the ones that matter.

   **What does not change with granularity:** whatever is submitted is decided before its effect, and both
   allow and deny reach one chain. One trail per system, not one log per pipeline.

   **Steps are not the same thing as chain records.** A step's status and its real output belong on the RUN
   record, which is where someone looks to see what a run did. The chain carries decisions, not progress.
2. **Untrusted step logic runs in a sandbox** (memory-limited, execution-limited, no network) rather than
   trusted on a build agent. A tool from a registry is not automatically trusted just because it was
   approved once.

## 9. Migration order (from a conventional CI system)

1. **Map the concepts before writing code** — config file → manifest; shared library → behaviours + tools;
   global overrides → layered config; factory → behaviour selection; worker phases → behaviour phases.
2. **Port ONE behaviour family end to end** (the simplest style), with `run()` final and the door call
   inside it. Prove a deny stops a phase.
3. **Add the other styles** as behaviour families, not as branches inside one behaviour.
4. **Move step logic into tools** — versioned, sandboxed — one phase at a time. Keep the old path working
   until each tool is proven.
5. **Extract the domain layer last**, from what the first real team revealed is shared.

## 10. Acceptance checks

1. **`run()` is `final`** and every governance call is inside it. A team cannot skip the door by overriding.
2. **A team behaviour overrides ≤ 2 hooks.** More means the base is missing something.
3. **A second repository style is a new behaviour family**, not an `if` inside an existing one.
4. **A team DTO restates no base field.**
5. **An unknown chain or a cycle fails before any step runs.**
6. **Every phase — not just the run — has a chain record**, allow or deny.
7. **A template/behaviour/tool is referenced by version**, and upgrading is a version change plus an
   approval record.
8. **No saga compensates another service's work.** If a compensation reaches across a service boundary, the
   saga boundary is in the wrong place.
