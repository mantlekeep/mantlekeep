# Templates, behaviours and workers — the composition model

**Read this with `layering.md`.** That doc says *who owns what* across three layers; this one says *how a
pipeline is actually assembled*, and it is the more important of the two. A pipeline is not a list of
steps — it is a **template** instantiated with a **behaviour**, whose steps are executed by **workers**.

Nothing here is novel on its own: template-method, factory-selected strategies, workers executing phases,
and dependency graphs run in topological order are all long-established. The contribution is the
**arrangement**, plus two additions — **every phase passes through the door**, and step logic runs in a
**sandbox** rather than trusted on the build agent. It is also a practical migration target for teams
moving off a conventional CI system, because the concepts map one to one.

```
manifest ──▶ template + behaviour ──▶ composed DAG ──▶ each step through the DOOR
                                                          └─▶ worker executes ──▶ hash-chain
```

---

## 1. The four concepts (do not collapse them)

| Concept | What it is | Who owns it |
|---|---|---|
| **Manifest** | the per-repository input: what to build, the chains, the values | the repository |
| **Template** | the reusable pipeline *shape* + its config DTO | generic / domain layer |
| **Behaviour** | the *lifecycle*: which phases run, in what order, with what semantics | selected per style; overridden per team |
| **Worker** | executes one phase against real tooling | generic, with per-style implementations |

**Composition is:** the manifest selects a behaviour → the behaviour orchestrates workers → each phase is
submitted to the door before it runs.

**Why they must stay separate:** a lifecycle is not universal. Building a service, applying
infrastructure-as-code, and migrating a database have genuinely different phases, different parameters, and
different release restrictions. A framework that hardcodes one lifecycle forces every team into it; that is
the mistake this model exists to avoid.

## 2. Behaviour selection is driven by repository style

Different repositories legitimately want different pipeline shapes:

| Style | Shape |
|---|---|
| `single-project` | one project per repository — a single linear lifecycle |
| `multi-project-independent` | many projects, each built independently |
| `multi-project-linked` | many projects with declared dependencies — a DAG, run in parallel levels |

The manifest declares the style; a **factory** selects the matching behaviour family. Teams keep their
pipeline shape — the framework does not impose one lifecycle on everyone.

## 3. The template-method rule (this is the "no rewrite" proof)

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

Two properties follow, and both are the point:

- **The skeleton is sealed; the steps are open.** `run()` is `final`, so a team cannot reorder phases, skip
  the door, or drop a gate. Everything a team legitimately needs to change is a `protected` hook.
- **Override exactly one method.** A team subclass changes what differs and inherits everything else:

```java
public final class TeamBehaviour extends DomainBehaviour {
    @Override protected void verify(PipelineContext context) { /* the team's own check */ }
    // packaging, containerising, phases — all inherited
}
```

Copy-paste is the failure mode this kills. **If a team is overriding more than two hooks, the base is wrong
— fix the base, for everyone.**

## 4. Config DTOs inherit too

The type system should mirror the config cascade: a template/config DTO extends a base; a team **adds
fields** and never restates the base.

```java
public class TemplateConfig      { /* generic fields */ }
public class DomainConfig  extends TemplateConfig { /* domain additions */ }
public class TeamConfig    extends DomainConfig   { /* team additions only */ }
```

A team DTO that redeclares a base field is a fork in disguise — the values drift and nobody notices which
one wins.

## 5. Packages state the layer

One direction, never reaching up:

```
com.acme.sdlc          ← generic (the framework you ship)
com.acme.domain.sdlc   ← domain layer (shared across teams)
com.acme.team.sdlc     ← a specific team
```

Group id names the layer's owner; the package path states the layer. **The base is what you ship and
version; the overrides belong to the team.** No flat default package.

## 6. Chains become a DAG

For the linked style the manifest declares chains and dependencies
(a `dependsOn` list per project). Normalise them into a subproject DAG and run
**topological levels in parallel**, preserving the manifest's order within a level.

**Fail fast, loudly:** an unknown chain, a dependency cycle, or an ambiguous target must abort before any
step runs. A pipeline that silently drops a subproject is worse than one that refuses to start, because
nobody notices the thing that stopped being built.

## 6b. The execution unit — govern per phase, execute per GROUP

**The mistake this section exists to prevent:** assuming that because every phase is governed separately,
every phase must *run* separately. That produces one pod per phase, and a pipeline that spends most of its
life waiting for schedulers and image pulls. A conventional CI agent runs a whole pipeline in one place and
feels fast for exactly this reason.

**Two boundaries, and they are orthogonal:**

| | Granularity | Cost |
|---|---|---|
| **Governance boundary** — what needs a decision | **per phase**, always | one call to the door: milliseconds |
| **Execution boundary** — what shares a process, filesystem, workspace | **per group**, configurable | a new unit: scheduling + image pull + start, often tens of seconds |

Governing finely is cheap. Executing finely is expensive. Coupling them makes governance look costly when
it is not, and invites someone to "fix performance" by governing less — the worst possible trade.

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

`branch` is the default because it matches what a DAG already tells you: **branches that declare no
dependency on each other have no reason to share a workspace, and every reason to run at the same time.**
Phases that *do* depend on each other pass work through the filesystem, so putting them in one unit is both
faster and simpler than staging artefacts between pods.

This is a well-trodden conclusion, not a novel one: mature Kubernetes-native CI systems group a task's
steps into a single pod as sequential containers over a shared workspace, and use separate pods for
independent tasks.

### The mode never changes whether something is governed

**Every phase submits to the door in every mode.** The mode decides *where* work runs, never *whether* it
was decided. A change that reduces the number of execution units must not reduce the number of decisions on
the chain — and if a chain shows fewer records after a performance change, that is a bug, not a
optimisation. Make it an assertion in the test suite.

### Isolation is a security decision, not only a performance one

Phases sharing a unit share a filesystem. That is fine for phases that trust each other — a build feeding a
verify. It is **not** fine for a phase that executes untrusted code, which must either get its own unit or
run inside the sandbox.

That is precisely what the sandbox is for (§8): a tool from a registry is untrusted regardless of the pod
it sits in, so sandboxing lets untrusted work stay in a shared, fast unit instead of paying pod isolation
for every step. **Sandbox the untrusted thing; do not isolate the whole pipeline to contain it.**

### If a unit is still slow to start

Fix the start-up cost rather than the granularity — but only after the granularity is right:

- keep a **warm pool** of pre-scheduled idle units, so a run claims one instead of waiting for a scheduler;
- keep unit images **small and pre-pulled** on the nodes that will run them;
- leave **scheduling headroom** so a unit is not waiting on a node that is itself being provisioned.

None of these help if the pipeline is still spawning a unit per phase — that is the ordering: granularity
first, warm start second.

## 7. Platform versus content — why this avoids a change request per change

The split that makes the model viable operationally:

| | Changes | Governed by |
|---|---|---|
| **PLATFORM** — the engine, the door, the sealed floors | rarely | a change request, like any core system |
| **CONTENT** — a template, a behaviour, a tool | often | the **door**: approval + hash-chain record |

A content change is a **versioned artifact**, approved at the door — **and that approval record is the
change evidence.** No fresh change request per behaviour change. This is what a stable platform plus
versioned shared logic already achieved, made explicit and auditable rather than conventional.

Practical consequence: **templates, behaviours and tools are versioned artifacts, pinned by version** — not
edited in place. A team upgrades by moving to a new version, and the move itself is recorded.

## 8. Where the door and the sandbox enter

Two things this model adds over a conventional CI system, and the only two:

1. **Governance granularity — decide this deliberately, because both answers are defensible.**

   | | What reaches the chain | Suits |
   |---|---|---|
   | **Transition-level** *(the usual first slice)* | propose · approve · reject — two or three records per run | one approval authorises the whole run; no floor needs step-specific data |
   | **Phase-level** *(the fuller form)* | every phase, each with its own parameters | a floor must gate one phase differently from another — "deploy to prod" separately from "build" |

   **Start at transition level.** It is simpler, it is honest, and it is enough when the approval genuinely
   covers the run. Move to phase level when a floor needs to inspect a *particular* phase's parameters —
   that requirement is what tells you, rather than a preference for more records.

   The trade is real in both directions: transition-level means an approved run executes its phases
   unchecked, so the approval must be worth that. Phase-level means more records, and records nobody reads
   dilute the ones that matter.

   **What does not change with granularity:** whatever IS submitted is decided before its effect, and both
   allow and deny reach one chain. One trail per system, not one log per pipeline.

   **Steps are not the same thing as chain records.** A step's status and its real output belong on the RUN
   record, which is where someone looks to see what a run did. The chain answers a different question — was
   anything altered — so it carries decisions, not progress.
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

## 10. Acceptance — how you know the model is real

1. **`run()` is `final`** and every governance call is inside it. A team cannot skip the door by overriding.
2. **A team behaviour overrides ≤ 2 hooks.** More means the base is wrong.
3. **A second repository style is a new behaviour family**, not an `if` inside an existing one.
4. **A team DTO restates no base field.**
5. **An unknown chain or a cycle fails before any step runs.**
6. **Every phase — not just the run — has a chain record**, allow or deny.
7. **A template/behaviour/tool is referenced by version**, and upgrading is a version change plus an
   approval record.

If (1) fails, nothing else in this document matters: an overridable skeleton means governance is optional.
