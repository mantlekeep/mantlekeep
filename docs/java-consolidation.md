# The two Java stacks — a consolidation note

**The problem in one line:** the same concepts are declared twice, in two module trees that do not know
about each other, so every fix has to be made twice and one copy is always missed.

This is not a style objection. It is the direct cause of a run of defects — each one a case where a
concept was corrected in one copy while the other kept the old behaviour, and both trees' tests passed
because each asserts against its own copy.

---

## 1. What is duplicated

| Concept | `sdks/java` | `mantlekeep-spring-boot` |
|---|---|---|
| **DoorClient** | `dev.mantlekeep.door.DoorClient` — `decide` · `submit` · `audit` · `verify` · `close` | `dev.mantlekeep.springboot.door.DoorClient` — `Mono<Decision> submit` |
| **Intent** | `(id, subject, action, resource, goal, parameters)` | `(action, resource, goal, env, scope, params)` |
| **Config** | `DoorConfig` | `DoorProperties` + `MantlekeepDoorProperties` + `IdentityProperties` |

A third `Intent` exists in the flat sidecar client (`dev.mantlekeep.Intent`), making three.

Note the Intents are not merely duplicated — **they disagree**. One carries a `subject` and no `env`; the
other carries `env` and `scope` and no subject. So the same word means two different things depending on
which import a file happens to have, and a reader cannot tell from the call site which is in play.

## 2. The evidence that this is the cause, not a tidiness complaint

Every one of these was a single conceptual fix applied to one copy:

- an identity header was configurable in one client and a constant in the other;
- `Intent.parameters` was widened for nested floor data in one Intent, leaving the others narrow;
- the caller header reached the wire from one client and was omitted entirely by the other;
- header names were defaulted in four places, so correcting two left two wrong.

**And the tests could not catch any of them.** Each tree's tests assert what *that* tree's client sends.
Both suites are green while the two implementations diverge, because nothing in the repository compares
them — nothing knows they are supposed to agree.

## 3. How it happened (worth recording, so the next fork is recognised early)

`sdks/java` came first: a framework-agnostic client, pure JDK, no Spring. `mantlekeep-spring-boot` came
later as a proper starter family — `-parent` conventions, a `-dependencies` BOM, typed starters — and
needed a door client that returned `Mono`. Rather than adapt the existing one, it declared its own.

That is a reasonable local decision that becomes a structural problem: the framework now contains the very
thing `layering.md` warns products against. **A reactive door client is an ADAPTER over the door client,
not a second door client.** The distinction is invisible on the day and expensive a month later.

## 4. The target shape — a pure-JDK spine, every framework a thin adapter

**This was the design all along.** `mantlekeep-door-client` is already pure JDK — `java.net.http`, no
Spring, no Reactor (verified: it imports nothing outside `java.*`). That is the generic spine. What went
wrong is not the design; it is that the WebFlux tree **drifted from it** — reimplementing `DoorClient`,
`Intent` and config with Reactor types instead of adapting the spine. Consolidation restores the intent,
it does not introduce a new one.

The rule that makes the framework serve any consumer: **no web-framework type in the core. Reactive,
servlet, native — all stay at the edge, in optional adapters. The door-client never learns which framework
is calling it.** This is the port-and-adapter discipline applied to the *consumer's* framework, the same
way `WorkerPort` applies it to the backend.

```
mantlekeep-adapter-spi        ports only, zero dependencies
        ↑
mantlekeep-door-client        DoorClient · Intent · Decision · DoorConfig   ← the ONE definition
        ↑                     PURE JDK (java.net.http) — assumes no framework
        ├── spring-boot-starter          blocking (Spring MVC) — thin bean wiring
        ├── spring-boot-starter-webflux  reactive — a thin Mono ADAPTER over the spine
        └── (no adapter needed)          Quarkus · Micronaut · plain Java · GraalVM native · Android
                                         depend on the door-client DIRECTLY
```

**Who depends on what:**

| Consumer | Depends on |
|---|---|
| Spring MVC (blocking) | door-client + a thin MVC starter (bean wiring only) |
| Spring WebFlux (reactive) | door-client + the webflux adapter (wraps calls in `Mono`) |
| Quarkus / Micronaut / plain Java / Android | **door-client alone** |
| GraalVM native image | **door-client alone** — `java.net.http` is reflection-free and AOT-friendly; Spring/Reactor are not |

**Why WebFlux must not be the base:** `Mono` is one framework's vocabulary. Making it the base forces every
consumer — MVC, Quarkus, native — to depend on Reactor for nothing. WebFlux is the *only* flavour that even
needs an adapter, because it needs the `Mono` wrapper; MVC uses the spine's blocking `decide()`/`submit()`
as-is, and everything else uses the spine directly. Treating the reactive path as the base was exactly
backwards.

Concretely:

1. **Delete** `springboot.door.{DoorClient, Intent, DoorProperties}`. The starter family depends on
   `mantlekeep-door-client` for them.
2. **`WebClientDoorClient` implements the SDK's `DoorClient`**, adapting it to `Mono` at the edge. Reactive
   is a transport concern; it does not need its own vocabulary for what an intent is.
3. **One config record.** `DoorConfig` is the definition; the Spring-bound properties record binds
   configuration onto it, exactly as `MantlekeepProperties` already does in the other tree. `env` and
   `scope` from the springboot Intent become parameters, which is where the engine already reads them from.
4. **The flat sidecar client keeps its own `Intent`** — it is a deliberately dependency-free demo path, and
   coupling it to the SDK would defeat its purpose. Document that as intentional so it is not mistaken for
   a fourth copy to merge.

## 5. The test that prevents recurrence

The consolidation removes today's duplicates. **A test is what stops new ones**, and it must compare the
two paths rather than check each in isolation:

> Build a blocking client and a reactive client **from the same `DoorConfig`**, submit the **same
> `Intent`** to a recording HTTP server through each, and assert **identical method, path, headers and
> body**.

That single test would have caught every defect listed in §2. It fails the moment the two paths disagree
about anything that reaches the wire, which is the only thing a consumer can observe.

## 6. Sequencing — and when NOT to do this

**Do not attempt it under a delivery deadline.** It touches both trees, and a half-finished consolidation
is worse than the duplication: some call sites on the new types, some on the old, and the compiler unable
to tell you which are wrong because both still exist.

A safe order:

1. Add the cross-path wire-equality test **first**, against the current duplicated types. It will not
   compile as one test yet — write it as two, asserting the same expectations, so the divergence is
   visible before anything moves.
2. Move `Intent` and `Decision` to the SDK types in the starter family. Largest diff, smallest risk: types
   only, no behaviour.
3. Make `WebClientDoorClient` implement the SDK `DoorClient`.
4. Collapse the config records.
5. Delete the orphaned `springboot.door` package, and let the compiler find the stragglers.

Steps 2–5 are each independently shippable, and the test from step 1 guards every one.

## 7. Cost of not doing it

Every future change to the door contract — a header, a field, a serialisation rule — must be made twice,
by someone who knows there are two. The current state depends on that knowledge, and the record shows what
happens when it is missing: nine corrections, each half-applied.
