# Extending and overriding across configuration layers

A product is typically organised in layers so that shared logic is defined once and specialised through
configuration rather than copies:

```
   Layer 3   team-sdlc          a specific team's pipeline    ← thin: order, values, one or two steps
   Layer 2   domain-sdlc        a domain's shared rules       ← e.g. an engineering-domain layer
   Layer 1   generic-sdlc       the product                   ← flow, states, saga, ports, endpoints
   ─────────────────────────────────────────────────────────
   Layer 0   the framework      door · chain · floors · ports ← a binary dependency, untouched
```

Each layer adds to and overrides the layer below through configuration; it does not edit the layer below.
A change needed by every team belongs in the shared layer, not in a copy.

If Layer 1 ships a new version, a team should be able to pick it up by bumping a version rather than
re-applying local changes.

This applies to both the backend and the UI; the mechanisms differ.

> **Companion: `behaviours.md`** — templates, behaviours and workers: how a pipeline is assembled. Useful
> when migrating from an existing CI system.

---

## 1. What each layer owns

| Layer | Owns | Must NOT contain |
|---|---|---|
| **1 · generic** | the flow (propose → approve → saga), states, the generic saga runner, the ports, the endpoints, the defaults | any domain vocabulary, any team's values, any specific tool |
| **2 · domain** | steps and floors shared by a whole domain; port implementations for that domain's tooling | any single team's thresholds, names, or order |
| **3 · team** | step order, actual values (caps, registries, approvers), at most one or two team-only steps | anything another team would also want — that belongs in Layer 2 |

Placement guide: if two teams would both want it, it belongs in Layer 2 or 1. If it names a specific team,
it belongs in Layer 3.

Layering is a dependency relationship, not a directory one. Sibling modules in one build, or one repo per
layer publishing versioned artifacts, are both valid. Many regulated organisations use a repo per layer,
because layers have different owners and release cadences and a version bump between them is an auditable
event. The framework itself reaches you the same way: a pinned binary dependency, not source in your tree.
Do not add a lower layer's source to your tree to make editing it easier — that produces a fork.

## 2. The four extension seams (Spring)

A higher layer extends a lower one through one of these four mechanisms, without editing the lower layer.

### Seam A — override a bean (`@ConditionalOnMissingBean`)

Layer 1 supplies a default; a higher layer declaring its own bean wins, with no change to Layer 1.

```java
// Layer 1 — the default, marked overridable
@Configuration
class RunStoreConfig {
    @Bean
    @ConditionalOnMissingBean            // ← the seam
    RunStore runStore() { return new InMemoryRunStore(); }
}
```
```java
// Layer 3 — a durable store, no edit to Layer 1
@Configuration
class TeamRunStoreConfig {
    @Bean
    RunStore runStore(DataSource dataSource) { return new JdbcRunStore(dataSource); }
}
```

Apply it to every port: `RunStore`, `WorkerPort`, the step implementations. A port without this annotation
cannot be replaced by a higher layer.

### Seam B — the behaviour

> **See `behaviours.md` for this seam.** What follows is the minimum. A lifecycle — which phases exist, with
> what parameters and release rules — differs between building a service, applying infrastructure, and
> migrating a database, and a flat list of steps cannot express that. Use a **template-method behaviour**
> (`final run()` + `protected` hooks) and select the behaviour family by repository style; the bean list
> below is the simple case for one style.

Make a step a **named bean**, and the pipeline an **ordered list of names in configuration**:

```java
// Layer 1 — generic steps, each a named bean, each overridable
@Bean @ConditionalOnMissingBean(name = "buildStep")
Step buildStep(WorkerPort worker) { return new BuildStep(worker); }
```
```java
// Layer 2 — a domain adds a step of its own
@Bean Step complianceStep(...) { return new ComplianceStep(...); }
```
```yaml
# Layer 3 — the team chooses which steps run, and in what order
acme:
  pipeline:
    steps: [ scanStep, buildStep, complianceStep, verifyStep ]
```

The runner resolves each name from the bean registry and runs them in the given order. This gives three
properties: a team can **reorder** without code, a domain can **add** without touching Layer 1, and **an
unknown step name fails at startup** rather than being skipped silently.

### Seam C — union the policy

Governance is data, and the layers **union**: `baseline ∪ domain ∪ team`. Point the runtime at each layer's
document (the policy directory accepts a path list); no layer edits another's file.

```
ACME_POLICY_DIR=/policy/generic.json:/policy/domain.json:/policy/team.json
```

A floor added by a lower layer **cannot be removed** by a higher one — that is what makes a floor a floor
rather than a default. Tightening is allowed; loosening is not, and a team document that tries it fails at
load.

### Seam D — configuration and naming

Values live in properties, layered by Spring profile (`application.yml` → `application-domain.yml` →
`application-team.yml`). The property prefix is the product's own (`acme.*`), declared once in the launcher.

None of these four seams involves copying a file.

## 3. The same layers in the UI

Same rule, different mechanism — the UI composes rather than inherits:

```
   Layer 3   team-console       branding, which panels appear, labels
   Layer 2   domain-console     domain-specific panels and vocabulary
   Layer 1   console            the audit chain view, run list/detail, approve/reject
   ─────────────────────────────────────────────────────────────────
   Layer 0   the door's API     /api/audit, /api/runs, /api/govern
```

- **Layer 1** owns the console blueprint: the chain view, run detail, the refusal rendering, the
  accessibility guarantees. Domain-neutral throughout.
- **Layer 2** registers additional panels and supplies domain wording. It does not modify Layer 1's files.
- **Layer 3** is configuration: brand values, which panels are enabled, label overrides.

Concretely, with the zero-build constraint: Layer 1 exposes a small registration point
(`registerPanel(name, renderFn)`) and reads a config object for branding and enabled panels. Layers 2 and 3
are additional `.js` files loaded after it that *register* things, rather than edited copies of it. A change
needed inside Layer 1's chain view belongs in Layer 1.

**The accessibility rules are Layer 1 and non-negotiable at every layer above.** A team layer may not
introduce colour-only status, audio, or a control that cannot be reached by keyboard.

## 4. Build order

1. **Layer 1, alone, working end to end** — with the defaults, and every port marked
   `@ConditionalOnMissingBean`.
2. **Seam B** — steps as named beans plus a configured order, replacing any hardcoded list. Verify by
   reordering the pipeline **in configuration only**.
3. **Layer 3 for one real team**, using only the four seams. Anything you have to reach past a seam to do is
   a missing seam in Layer 1 — add it there.
4. **Layer 2** last, extracted from what Layer 3 showed is shared.

## 5. Acceptance checks

1. **Layer 1 upgrades cleanly:** bump Layer 1, rebuild the team app, and nothing in Layer 3 changes.
2. **Reorder without code:** a team changes step order by editing configuration only.
3. **Add without touching Layer 1:** a domain adds a step; Layer 1's source is untouched by the diff.
4. **Override without touching Layer 1:** a team swaps the store or the worker; Layer 1's source is untouched.
5. **Floors only tighten:** a team document attempting to loosen a lower floor fails at load.
6. **A wrong step name fails at startup**, never silently skipped.
7. **No duplicated files** — `diff` across the layers finds no copied class or copied page.
