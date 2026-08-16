# Changelog

All notable changes to MantleKeep are documented here.
Format: [Keep a Changelog](https://keepachangelog.com); versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.2] — 2026-08-14

Patch — a **security-hygiene** release: clears a consumer's **SonarQube Quality Gate** and a batch of
Go **stdlib CVEs**. Go-only; no API change.

### Security
- **Go toolchain bumped `1.26.4 → 1.26.6`** (go.mod `go` directive + CI). Clears four `govulncheck`
  standard-library advisories reachable from the engine — **GO-2026-6090 / 6089 / 5972** (crypto/tls,
  net/http, encoding/asn1; fixed in 1.26.6) and **GO-2026-5856** (crypto/tls; fixed in 1.26.5). With
  `GOTOOLCHAIN=auto` a consumer on an older 1.26 patch auto-fetches 1.26.6 — no manual step. Verified:
  `govulncheck ./...` → *No vulnerabilities found*.
- **No audit DB opened in a predictable shared-temp path** (SonarQube **S5443**). Three sites opened
  an audit/door DB at `os.TempDir()/<fixed-name>`; each now creates a unique **owner-only** directory
  via `os.MkdirTemp("", "…-*")` and removes it on exit — closing a symlink / pre-create race on a
  world-writable path. Sites: `cmd/mantlekeep`, `cmd/acme-govern`, `internal/app`.
- **Dev-login session cookie is now always `Secure`** (SonarQube cookie security hotspot). It was
  `Secure: request.TLS != nil`; it is now a literal `true`. Browsers still store a `Secure` cookie for
  `http://localhost`, so the loopback dev-login path is unaffected; this cookie is dev-only,
  off-by-default (`Options.DevLogin`), and already set `HttpOnly` + `SameSite=Lax`.

### CI — keep it CVE-clean by construction
- **Pinned CI tool + build versions** (SonarQube **S8545 / S8544**, supply-chain). The `go install`
  security tools are pinned exact — gosec `v2.28.0`, staticcheck `v0.7.0`, govulncheck `v1.7.0` — and
  the PyPI `build` is pinned, instead of `@latest` / unpinned. A compromised upstream `@latest` can no
  longer enter the pipeline silently; Dependabot bumps them under the gate. (govulncheck's vuln DB is
  still fetched live, so pinning the tool does not blind it.)
- **SonarQube runs as a CI gate** (`sonar.yml`, CI-based) so it reads `sonar-project.properties` — the
  analysis scope is the three product dirs, not CI workflows or demo code. Fails on a red Quality Gate.
- **Weekly security re-scan** (`security.yml` cron) so a CVE disclosed *after* the last commit reds the
  gate within the week, on unchanged code — not only on the next push.
- **Dependabot** opens weekly dependency + pinned-Action update PRs; the security gate proves each bump
  CVE-clean before it can merge (automation proposes, the gate decides).

### Adopt
- Bump `0.1.1 → 0.1.2`. **Go module only** — tag `mantlekeep-control/v0.1.2`. The bare `v0.1.2` is
  intentionally **not** cut: it would trigger the Java Maven Central publish, but the Java surface is
  unchanged (no findings there) and stays `0.1.1`. Go resolves `@v0.1.2` through the prefixed tag.
  Drop-in; no API change, no new required config.

## [0.1.1] — 2026-08-09

Patch — the door client now fails **closed** on a hung door.

### Fixed
- **`WebClientDoorClient` now applies its `responseTimeout`** (default 10s, `mantlekeep.door.response-timeout`).
  The property was declared but never applied, so a HUNG door — one that never answers, as opposed to erroring —
  would hang the governed call instead of failing closed. A timeout is now treated as a transport failure and
  mapped to a `DoorException`, so the governed method never runs while the door is unresponsive.

### Adopt
- Bump `0.1.0 → 0.1.1`. **No API change, no new required config** (safe default) — a pure, drop-in patch.

## [0.1.0] — 2026-08-05

First stable release. Consolidates the rc.1–rc.7 line into the `0.1.0` API.

### Added / Changed since rc.7

- **Maven Central publishing** for both the Java SDK (`sdks/java`) and the Spring Boot starter
  family (`mantlekeep-spring-boot`). Snapshot pipeline verified end-to-end (signing, snapshot repo,
  namespace enablement).
- **Spring CVE-float:** the starters pin only MantleKeep's own artifacts; Spring/reactor/platform
  deps are version-less in the published pom, so a consumer's own Spring Boot BOM floats them to any
  patched release — MantleKeep never caps the consumer's Spring version. Achieved via ci-friendly
  `${revision}` versioning + `flatten-maven-plugin` (`resolveCiFriendliesOnly`).
- **Docs:** the guides are matter-of-fact reference (how to use and extend) rather than design essays.
- Dev-login session cookie sets `Secure` under TLS (CodeQL cookie-secure fix).

### Guarantees (unchanged, the point of the framework)

- **Govern before you execute.** Every human/AI action passes one door, decided against policy and
  recorded on an append-only hash-chain, before any side effect.
- **The sealed floor.** An AI can never approve its own work — structural, not configuration.

## [0.1.0-rc.7] — 2026-08-05

### Added

- **`SagaRecorder.dispatched(operationId, subject, detail)`** — a first-class saga step for the
  ROUTING phase: between `requested` and `executed`, for when execution is dispatched (routed to an
  executor such as an in-zone agent) rather than run in-process. Records `step="dispatched"`,
  `status="routed"`; `detail` carries the routing target. Products modelling an async/routed phase
  record it instead of overloading `executed`.

### Fixed

- Removed a stray build binary (`mantlekeep-control/mantlekeep`) accidentally tracked, and gitignored it.

## [0.1.0-rc.6] — 2026-08-04

### Changed

- **Go module path is now `github.com/mantlekeep/mantlekeep/mantlekeep-control`** (was
  `mantlekeep.dev/control`). A github-based path resolves through a Nexus github proxy and public
  `go install` with **no external-domain dependency in the build chain** — a vanity domain would need
  `mantlekeep.dev` reachable or pre-cached, a fragility and audit surface in an air-gapped deployment.
  `mantlekeep.dev` remains the brand/docs domain and Maven-Central verification, not the build path.
  **Breaking for Go importers** — update imports to the new module path. (Java `dev.mantlekeep:*`
  coordinates and the Python client are unaffected.)

## [0.1.0-rc.5] — 2026-08-04

Saga/timeline recording promoted into the framework — the runtime trail every governed service needs,
now a shared primitive instead of hand-rolled per product.

### Added

- **Saga/timeline recording** (`dev.mantlekeep.springboot.saga`). `SagaRecorder` emits a governed
  order's runtime steps (the environment + the real command + the outcome), gated by `RecordingLevel`
  (`NONE` / `DECISIONS` / `STEPS` / `FULL`; config `mantlekeep.saga.recording`, default `STEPS`). This is
  the trail of what an executor DID — distinct from the audit chain (the DECISION) and the operation
  ledger (the STATE). `SagaTimeline` is a port (`InMemorySagaTimeline` is the default; a StorePort-backed,
  chain-correlated adapter drops in later); both auto-configure and a product overrides by defining its
  own bean. Recording tunes what lands on the timeline — it NEVER gates whether an order goes through the
  door: a run can be silent but never ungoverned.

## [0.1.0-rc.4] — 2026-08-04

Configurable authority, transition-level governance, and a release surface hardened to a clean
security scan — the operability and integrity a real deployment needs to trust the door.

### Added

- **Deployment-configurable role ladder.** The authority vocabulary is no longer hardcoded: a
  deployment declares its own role names and ranks in a policy layer's `roles` map (or in code via
  `WithRoleLadder`), so a host renames tiers in config with no fork. Default-preserving — omit
  `roles` and the built-in five tiers apply unchanged. The core now holds no hardcoded role name
  beyond the default it ships.
- **Transition-level governed execution (`runUnder`).** `GovernedExecutionScope.runUnder(approvalToken,
  work)` in the WebFlux SDK runs a saga's steps beneath ONE approval — approve the run once, steps
  execute under it, no per-step door re-submission. Fails closed when no approval scope is present.
- **Door decision logging.** One structured `slog` line per finalized decision (outcome, action,
  subject, policyId, `via` when delegated, `category`+reason on a deny) — allow at info, deny at
  warn. Metadata only: it never logs intent params or any token. Live operator view alongside the
  tamper-evident audit chain.
- **Policy-config fingerprint.** Each loaded policy layer logs a `sha256` over its raw bytes, so a
  `policy.json` change is visible in the boot log without a separate schema file.

### Changed — security & integrity

- **Policy config is now fail-CLOSED.** A config file that is set but invalid refuses startup instead
  of being silently ignored (the previous fail-open): unknown keys are rejected
  (`DisallowUnknownFields`), and a role named in `actionRoles` but absent from the ladder is a hard
  error. A hot-reload of an invalid edit keeps the last-good snapshot (fails static, never open).
- **Go engine hardened to a clean security scan.** `gosec` and `staticcheck` are zero: operator
  config reads route through a single validated `safeio` door (path cleaned, traversal rejected),
  plus integer-overflow, HTTP-timeout, and error-handling fixes.
- **Java SDK + Spring Boot pass SpotBugs + FindSecBugs with zero security findings**, and a permanent
  `security.yml` CI gate now runs SpotBugs/FindSecBugs (Java) and gosec/staticcheck/govulncheck (Go)
  on every push and PR, failing on any finding.
- **The WebFlux door client fails closed.** `allow` is trusted only on an explicit `outcome:"allow"`
  over a 2xx response; an unrecognized or blank outcome maps to DENY (previously a 2xx with an
  unknown outcome inferred ALLOW — a latent fail-open), and an `allow` claimed on a non-2xx is not
  trusted.

## [0.1.0-rc.3] — 2026-08-01

Consolidation and a typed denial contract, plus a third language SDK — the surface a real
consumer's gap report asked for, made structural rather than guessed.

### Added — a Python door client

- **`sdks/python`** — a pure-standard-library Python door client: one more peer client over the
  same `/api/govern` wire contract (Java and Python today, a Rust client to follow), each thin and
  idiomatic in its own runtime. Zero runtime dependencies (it speaks `/api/govern` with `urllib` + `json`),
  so it adds nothing to an air-gapped image. Carries the same rich `Decision`, identity-as-header
  (never body), configurable rebrandable headers, and a `GovernedWorker` that runs work only on
  allow. Verified by running: the integration test builds and serves the Go door and drives
  allow / policy-deny / validation-deny through it, and proves the worker refuses work on a deny.

### Added — the denial is typed at its source, not guessed at the wire

- **A generic `DenialCategory` on the engine `Decision`** (`floor` | `separation_of_duties` |
  `identity` | `action_not_allowed` | `validation` | `policy_error`) — the same register of core
  vocabulary as `DecisionAction` and `Role`, naming governance shapes and no product/action/env/role.
  The engine stamps it at the point of denial; the wire maps each category to its transport code.
  Substring-matching the human reason survives only as a fallback for an external evaluator
  (OPA/Cedar) that returns a bare `Decision`.
- **This fixed a real misclassification:** the sealed-floor denial "AI agents cannot approve"
  matched no separation-of-duties substring and fell through to `DENY_POLICY_ERROR` — the least
  specific code, for the single most important denial in the system. It is now
  `DENY_SEPARATION_OF_DUTIES`, structurally, whatever the reason wording.

### Changed — one Java surface, not two

- **The Spring Boot family now consumes the pure-JDK door-client spine** — `springboot.door.Intent`
  and `springboot.door.Decision` are deleted; there is one `Intent` and one `Decision`. A
  cross-path wire-equality test asserts the blocking and reactive clients send byte-identical
  requests, so the two paths can never silently diverge again (the cause of several rc.2 defects).
  The reactive client keeps its own class for now — collapsing it onto the blocking interface needs
  `OnBehalfOf` resolved reactively, and a blocking `.block()` would drop delegation attribution;
  the wire-equality test guards against regression until then.
- **The native/FFI `Decision` contract is pinned** (`docs/native-core-contract.md`) to the canonical
  rich shape, with the Go core as the oracle — so a future Rust core produces byte-identical
  decisions rather than a guessed shape.

### Added — a publish gate

- **CI** runs `go vet` + `staticcheck` + `govulncheck` + tests on the engine, a
  **near-zero-dependency guard** (fails if direct non-stdlib deps exceed the baseline of 1, `bbolt`),
  both Java build systems, and the Python SDK tests. A `sonar-project.properties` supports
  open-source SonarQube scanning. The only govulncheck finding is a Go-stdlib advisory fixed in
  go1.26.5, cleared by pinning the toolchain — no MantleKeep code or dependency is vulnerable.

## [0.1.0-rc.2] — 2026-08-01

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

[Unreleased]: https://github.com/mantlekeep/mantlekeep/compare/v0.1.0-rc.3...HEAD
[0.1.0-rc.3]: https://github.com/mantlekeep/mantlekeep/compare/v0.1.0-rc.2...v0.1.0-rc.3
[0.1.0-rc.2]: https://github.com/mantlekeep/mantlekeep/compare/v0.1.0-rc.1...v0.1.0-rc.2
[0.1.0-rc.1]: https://github.com/mantlekeep/mantlekeep/releases/tag/v0.1.0-rc.1
