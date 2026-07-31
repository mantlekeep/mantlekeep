# Three-layer products — generic → domain → team

**The model.** One service is built once and specialised twice, by **composition**, never by copying:

```
   Layer 3   team-sdlc          a specific team's pipeline    ← thin: order, values, one or two steps
   Layer 2   domain-sdlc        a domain's shared rules       ← e.g. an engineering-domain layer
   Layer 1   generic-sdlc       the product                   ← flow, states, saga, ports, endpoints
   ─────────────────────────────────────────────────────────
   Layer 0   the framework      door · chain · floors · ports ← a binary dependency, untouched
```

Each layer **adds and overrides**; none edits the layer below it. If a team needs a change in Layer 1, that
change belongs in Layer 1 for everyone — not in a copy.

**The test for whether you got it right:** can Layer 1 ship a new version and every team pick it up by
bumping a version? If a team has to re-apply its changes, the layering has already failed and you have
forks wearing layer names.

This applies identically to the backend and the UI. The mechanisms differ; the rule does not.

> **Companion: `behaviours.md`** — templates, behaviours and workers. This doc says who owns what; that one
> says how a pipeline is assembled, and is the more important of the two for a migration from an existing
> CI system.

---

## 1. What each layer owns

| Layer | Owns | Must NOT contain |
|---|---|---|
| **1 · generic** | the flow (propose → approve → saga), states, the generic saga runner, the ports, the endpoints, the honest defaults | any domain vocabulary, any team's values, any specific tool |
| **2 · domain** | steps and floors shared by a whole domain; port implementations for that domain's tooling | any single team's thresholds, names, or order |
| **3 · team** | step order, actual values (caps, registries, approvers), at most one or two team-only steps | anything another team would also want — that belongs in Layer 2 |

**The smell test for a misplaced thing:** if two teams would both want it, it is Layer 2 or 1. If it names a
specific team, it is Layer 3. A "generic" layer that mentions a business domain is mislabelled.

**Layering is a dependency relationship, not a directory one.** Sibling modules in one build, or one repo
per layer publishing versioned artifacts — both are the same architecture. Most regulated organisations end
up with a repo per layer, because layers have different owners and release cadences, and a version bump
between them is an auditable event rather than a merge. That is how the framework itself reaches you: a
pinned binary dependency, not source in your tree. **Never add a lower layer's source to your tree to make
editing it easier** — that is a fork wearing a layout excuse.

## 2. The four seams (Spring)

Everything a higher layer does, it does through one of these four. There is no fifth, and there is no
editing downward.

### Seam A — override a bean (`@ConditionalOnMissingBean`)

Layer 1 supplies a default; a higher layer declaring its own bean wins, with no change to Layer 1.

```java
// Layer 1 — the honest default, marked overridable
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

Apply it to **every port**: `RunStore`, `WorkerPort`, the step implementations. A port without this
annotation is a port a higher layer cannot replace — which makes it an accidental fork point.

### Seam B — the behaviour (NOT a hardcoded list, and NOT merely a list of beans)

> **Read `behaviours.md` for this seam.** What follows is the minimum; the behaviour model there is the
> real mechanism, because a *lifecycle* — which phases exist, with what parameters and release rules —
> differs between building a service, applying infrastructure, and migrating a database. A flat list of
> steps cannot express that. Use a **template-method behaviour** (`final run()` + `protected` hooks) and
> select the behaviour family by repository style; treat the bean list below as the degenerate case for
> one simple style.

**This is the seam that has to be built before layering works at all.** A pipeline written as
`private static final List<StepDef> STEPS = List.of(...)` cannot be extended by anyone — a team's only
option is to copy the class, which is the fork the model exists to prevent.

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

The runner resolves each name from the bean registry and runs them in the given order. Three properties
follow from this, all of them the point: a team can **reorder** without code, a domain can **add** without
touching Layer 1, and **an unknown step name must fail at startup** — never be skipped silently, or a
pipeline quietly stops enforcing something.

### Seam C — union the policy

Governance is data, and the layers **union**: `baseline ∪ domain ∪ team`. Point the runtime at each layer's
document (the policy directory accepts a path list); no layer edits another's file.

```
ACME_POLICY_DIR=/policy/generic.json:/policy/domain.json:/policy/team.json
```

A floor added by a lower layer **cannot be removed** by a higher one — that is what makes a floor a floor
rather than a default. Tightening is always allowed; loosening is not, and a team document that tries it
must fail loudly at load.

### Seam D — configuration and naming

Values live in properties, layered by Spring profile (`application.yml` → `application-domain.yml` →
`application-team.yml`). The property prefix is the product's own (`acme.*`), declared once in the launcher.

**None of these four seams involves copying a file.** If you find yourself copying to specialise, you have
left the model.

## 3. The same three layers in the UI

Identical rule, different mechanism — the UI composes rather than inherits:

```
   Layer 3   team-console       branding, which panels appear, labels
   Layer 2   domain-console     domain-specific panels and vocabulary
   Layer 1   console            the audit chain view, run list/detail, approve/reject
   ─────────────────────────────────────────────────────────────────
   Layer 0   the door's API     /api/audit, /api/runs, /api/govern
```

- **Layer 1** owns everything in the console blueprint: the chain view, run detail, the refusal rendering,
  the accessibility guarantees. Domain-neutral throughout.
- **Layer 2** registers additional panels and supplies domain wording. It does not modify Layer 1's files.
- **Layer 3** is configuration: brand values, which panels are enabled, label overrides.

Concretely, with the zero-build constraint: Layer 1 exposes a small registration point
(`registerPanel(name, renderFn)`) and reads a config object for branding and enabled panels. Layers 2 and 3
are additional `.js` files loaded after it that *register* things — never edited copies of it. If a domain
needs a change inside Layer 1's chain view, that change is Layer 1's, for everyone.

**The accessibility rules are Layer 1 and non-negotiable at every layer above.** A team layer may not
introduce colour-only status, audio, or a control that cannot be reached by keyboard.

## 4. Build order (do not invert this)

1. **Layer 1, alone, working end to end** — with the honest defaults, and every port marked
   `@ConditionalOnMissingBean`.
2. **Seam B** — steps as named beans plus a configured order, replacing any hardcoded list. Prove it by
   reordering the pipeline **in configuration only**.
3. **Layer 3 for one real team**, using only the four seams. Whatever you have to reach past a seam to do
   is a missing seam in Layer 1 — fix it there.
4. **Layer 2** last, extracted from what Layer 3 revealed is shared. A domain layer designed before any
   team exists is guesswork; extracted afterwards, it is evidence.

## 5. Acceptance — how you know the layering is real

1. **Layer 1 upgrades cleanly:** bump Layer 1, rebuild the team app, and nothing in Layer 3 changes.
2. **Reorder without code:** a team changes step order by editing configuration only.
3. **Add without touching Layer 1:** a domain adds a step; Layer 1's source is untouched by the diff.
4. **Override without touching Layer 1:** a team swaps the store or the worker; Layer 1's source is untouched.
5. **Floors only tighten:** a team document attempting to loosen a lower floor fails at load, loudly.
6. **A wrong step name fails at startup**, never silently skipped.
7. **No duplicated files anywhere** — `diff` across the layers finds no copied class or copied page.

If (1) fails, you do not have layers. Everything else is detail by comparison.
