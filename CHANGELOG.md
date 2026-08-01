# Changelog

All notable changes to MantleKeep are documented here.
Format: [Keep a Changelog](https://keepachangelog.com); versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.1-rc.1] — 2026-08-01

Driven by a real consumer: a regulated organisation built a governed SDLC service against `0.1.0-rc.1`
with an in-house AI assistant, hit the framework's edges, and **failed closed rather than bypass
governance** — then returned a gap spec. This candidate closes the gaps that were defects, and records
the ones that are genuine new work. The bypass discipline holding under real pressure is the signal the RC
existed to produce.

### Fixed — identity reached the door, or it did not

- **`ServiceDoorClient` sent no caller identity at all.** It dropped `Intent.subject` and set no header, so
  the door saw an anonymous request and refused everything with `401` unless a dev-login cookie happened to
  exist. Identity now travels as a header (never the body — a body-supplied caller is forgeable).
- **`DoorClientFactory` dropped the configured header names**, so a branded deployment silently sent the
  framework's default names and the door refused them.
- **The identity header was `X-Mantlekeepkeep-User`** — a rename applied twice. Docs said `X-Mantlekeep-User`;
  a caller following the docs got `401`. A test now pins the literal names.
- **The on-behalf-of header was hardcoded** while its pair was configurable, so a rebrand renamed one and
  not the other — the door authenticated the service and silently dropped the delegation, recording the
  action against the service instead of the person. The pair now moves together.
- **Refused callers are now logged, and malformed identities refused at the boundary.** A request refused
  before the door produces no chain record, so this log is the only evidence it happened; and a caller name
  with control characters is rejected rather than sanitised, because it would otherwise become the subject
  on a tamper-evident record.

### Fixed — floors could not fire

- **`Intent.parameters` could not carry nested values,** so a `capped_map` resource floor — the primary
  session floor — could never trigger from Java, and failed **silently** (finding no map, it allowed the
  request). Parameters now carry arbitrary values; numbers and booleans keep their JSON types so a numeric
  cap compares numbers. The component is `Map<String, ?>`, so existing `Map<String,String>` callers still
  compile.
- **The policy namespace was hardcoded `mantlekeep.rbac`,** putting the framework's name into every audit
  record — the one place branding cannot be removed afterwards. It is now `MANTLEKEEP_POLICY_NAMESPACE`,
  defaulting to the brand name, then to `mantlekeep`.

### Added — govern-before-execute made structural

- **`GovernedWorker`** — the framework now owns the decide-then-dispatch sequence (`final`), and the Spring
  starter publishes it while **not** publishing the raw `WorkerPort`. A product can no longer casually
  execute outside governance, because the unwrapped executor is not an injectable bean.
- **`GovernedWorker.runUnder(approvalToken, work)`** — execute saga steps beneath a single transition-level
  approval, so a run approved as a unit does not re-ask the door per step (which would turn transition
  governance into phase governance). This was a real consumer's top blocker.
- **`app.Brand` / `BrandPrefix`** — a product declares its own name and config prefix without naming any
  framework variable; `frame.door.url` binds in `application.yml`, aliased at lowest precedence so an
  explicit `mantlekeep.*` value still wins.

### Corrected claims (the code no longer promises more than it delivers)

- The `ExecutionToken` was described as a signed cryptographic capability; it is an opaque, unsigned value —
  evidence of a decision, not a key. Said plainly now.
- `WorkerPort`/`sdk` comments claimed "structural" / "No bypass"; in-process governance is enforced by the
  framework owning the dispatch path, not by removing the ability to bypass it. See
  `docs/credential-brokering.md` for the control that removes the capability.

### Known limitations (unchanged from rc.1, restated)

- Embedded mode is not usable from Java (no native library is built); use `mode: service`. The Go embedded
  path via `doorkit` works today.
- **The Java surface is duplicated across two module trees** (`DoorClient`, `Intent`, door config declared
  twice; `Intent` three times, two disagreeing). A fix must be applied to both — the cause of several of the
  defects above. Consolidation is scoped in `docs/java-consolidation.md`; it is cross-build and awaits a
  build-topology decision.
- Saga step evidence and a typed denial taxonomy are specified by the consumer's gap report but not yet
  built. Design direction: saga records persist through `StorePort` (per-purpose bindable), correlated to
  the chain head; recording verbosity is env-scaled policy (dev light, production full), while
  govern-before-execute stays sealed at every level.

## [0.1.0-rc.1] — 2026-07-31

The first release candidate. **The API is not frozen** — this line exists so the shape can be exercised
against real consumers before anything is promised. Iteration happens inside the candidates.

**It promotes to `0.1.0` when:** the API has held for a full candidate cycle · every documented command
runs from a fresh clone · a real consumer has driven it end to end · no defect is known in the surface it
claims.

### Known limitations

- **Embedded mode is not usable from Java.** `mantlekeep.door.mode=embedded` resolves a native binding,
  and this build ships no native library — no `c-shared` target produces one. Java products should use
  `mode: service` against a door run with `mantlekeep serve`. A **Go** product embeds the core directly
  through `doorkit` with no native library, and that path works today.
- The `ExecutionToken` is an opaque random value: it records which decision authorised a piece of work,
  and is neither signed nor verified. It is evidence, not a key.
- **The Java surface is duplicated across two module trees** — `DoorClient`, `Intent` and door config are
  each declared twice (and `Intent` three times, with two of them disagreeing about their fields). A fix
  must therefore be applied to both, and each tree's tests pass while the copies diverge. See
  `docs/java-consolidation.md` for the target shape and migration order.
- Governance inside an executing process is enforced by the framework owning the dispatch path, not by
  removing the ability to bypass it. See `docs/credential-brokering.md` for the structural control.

### The governance core

- **One door.** `submit(intent) → allow | deny`, decided against policy, recorded, and returning an
  execution token only on allow. Govern first; execute second. A deny aborts before any side effect.
- **A tamper-evident chain.** Every allow *and* every deny is appended to a hash-chain in which each
  record covers the previous record's hash, so alteration is detectable by re-walking it.
- **A generic policy floor.** The engine knows zero product, action, environment or role names; grants and
  floors are supplied as data and unioned across layers. With no policy loaded the door denies — safe by
  default rather than permissive by default.
- **The sealed floor.** An AI can never approve its own work. This is not configuration.
- **Ports and adapters.** The core knows only the port; backend knowledge lives in an adapter selected by
  configuration and discovered through a registered set — never `Class.forName` on a config value.
- **An orchestrator spine.** Forward run with compensate-in-reverse rollback.

### Running it

- **`mantlekeep serve`** — the door as a service: `POST /api/govern`, `GET /api/audit`, and an opt-in dev
  login. It **fails closed twice**: it refuses to start without a way to identify callers, and refuses any
  request whose caller cannot be resolved, before the door is asked.
- **Delegation.** A service account can act *for* a person: the chain records the person as **subject** and
  the service as **via**. The control is an explicit delegator allowlist — a caller not on it is refused
  outright, never silently downgraded to acting as itself.
- **Embedded or service.** Embedded gives a process its own local door and chain, which is right for one
  sovereign zone; service gives many services **one shared chain**, which is what an auditor needs.

### SDKs

- **Java** — a config-driven `DoorClient` (`service` | `embedded`), the adapter SPI, worked example
  adapters, and a Spring Boot starter whose `@MantlekeepIntent` aspect governs a method before it runs.
- **Build with Gradle or Maven** — both first-class, producing identical artifacts, so a host that must
  build from source (an air-gapped rebuild, a patch on vendored source, an internal audit) can.

### White-labelling

- **`app.Brand`** — a product states its own name and environment prefix; **no framework variable name
  appears in branded product code**. `BrandPrefix` does the same for Java configuration, so
  `application.yml` speaks the product's name. Aliases apply at the lowest precedence, so adopting a prefix
  cannot silently change existing configuration.
- **A worked example** (`cmd/acme-govern`) builds a second branded binary from the unmodified core.

### Documentation

Adoption guides, the door's library and wire contracts, and the design notes that explain the shape:
layering a product across generic/domain/team, the template–behaviour–worker composition model, a shared
audit chain for replication, federated doors across zones, and the execution unit.

[Unreleased]: https://github.com/potkei/mantlekeep/compare/v0.1.1-rc.1...HEAD
[0.1.1-rc.1]: https://github.com/potkei/mantlekeep/compare/v0.1.0-rc.1...v0.1.1-rc.1
[0.1.0-rc.1]: https://github.com/potkei/mantlekeep/releases/tag/v0.1.0-rc.1
