# Changelog

All notable changes to MantleKeep are documented here.
Format: [Keep a Changelog](https://keepachangelog.com); versioning: [SemVer](https://semver.org).

## [Unreleased]

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

[Unreleased]: https://github.com/potkei/mantlekeep/compare/v0.1.0-rc.1...HEAD
[0.1.0-rc.1]: https://github.com/potkei/mantlekeep/releases/tag/v0.1.0-rc.1
