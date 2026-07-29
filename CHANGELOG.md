# Changelog

All notable changes to MantleKeep are documented here.
Format: [Keep a Changelog](https://keepachangelog.com); versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.3] — 2026-07-29

### Fixed
- **Fresh clones could not build the Go core.** Dropping the root `go.work`/`go.work.sum` in v0.1.1 also
  dropped the `/go.mod` hash lines those files carried for two indirect deps, so `go build ./cmd/mantlekeep`
  on a machine with a cold module cache failed with `missing go.sum entry for go.mod file`. `go.sum` is
  restored (and `cucumber/godog`, a phantom require imported by no package here, is gone). Verified with an
  isolated module cache: build clean, `go test ./...` 42 passed in 18 packages, `go vet` clean.

### Added
- **Maven build for the Spring Boot starter family too** (`mantlekeep-spring-boot/`) — v0.1.2 covered only
  `sdks/java`, leaving half the Java surface Gradle-only. Adds a parent POM + a real Maven **BOM**
  (`-dependencies`, importing the Spring Boot platform) + module POMs for `-core`, `-starter-webflux`,
  `-starter-ai`. The Gradle idioms map onto their Maven equivalents rather than being reinvented: convention
  plugins → parent POM, `java-platform` → BOM, `annotationProcessor` → `optional` dependency.
  Verified: `mvn install` green, **51 tests** pass; `./gradlew build` still green.
  (`mantlekeep-spring-boot-parent/` stays Gradle-only by design — in Maven a parent POM *is* that mechanism.)

## [0.1.2] — 2026-07-29

### Added
- **Maven build alongside Gradle — both first-class.** The Java SDK now ships `pom.xml` files (a parent
  aggregator + one per module), so a Maven shop can build the framework **from source** — the case that
  matters for air-gapped rebuilds, CVE patching on vendored source, and internal audit. Verified: `mvn install`
  green, 32 tests pass, identical `dev.mantlekeep:*:0.1.0` coordinates to the Gradle build, which stays green.
  (Maven *consumers* were never blocked — published artifacts were always ordinary POMs; this adds *building*.)
  Known limit: the legacy flat sidecar client (`sdks/java/src`) stays Gradle-only — a Maven parent cannot
  carry its own sources; the full `starter → door-client → adapter-spi` chain is Maven-buildable.

## [0.1.1] — 2026-07-28

### Changed
- **Go: `mantlekeep-control` is now a standalone module.** Removed the redundant single-module
  `go.work`/`go.work.sum` at the repo root (they only stitched the one module and left a stray root
  checksum file); `go.mod`/`go.sum` live in the `mantlekeep-control/` subproject. Build + vet verified
  green standalone (`GOWORK=off`).

### Added
- **Java SDK is now publishable to Maven** as `dev.mantlekeep:<module>:0.1.0` (jar + sources + POM,
  transitive `dev.mantlekeep` chain). Published to GitHub Packages first (no domain needed), upgradeable
  to Maven Central under the same coordinates once `mantlekeep.dev` is DNS-verified. Consume the whole
  governance stack with one line: `implementation "dev.mantlekeep:mantlekeep-spring-boot-starter:0.1.0"`.

## [0.1.0] — 2026-07-28

### Added
- **The generic governance core** — the one **door** (`submit(intent) → allow/deny`), the
  tamper-evident **hash-chain** audit, the **generic policy floor** engine (data-driven; the engine
  knows zero product, action, environment, or role names), and the **ports/adapters** seam
  (`WorkerPort`, `PolicyEvaluator`, `StorePort`, `AgentPort`).
- **Govern-before-execute** + the **sealed floor** — every action decided at one door before any side
  effect; AI can never approve its own work.
- **The orchestrator/saga spine** — forward run + compensate-in-reverse rollback.
- **Java SDK** — a config-driven `DoorClient` (`service` | `embedded` modes), the adapter **SPI** +
  `ServiceLoader` discovery, example adapters (in-memory store, allow-list policy), and the **Spring
  Boot starter** (`@MantlekeepIntent` aspect, identity resolution, a BYOK agent starter).
- **Configurable branding** via `*_BRAND_*` env (`GET /api/brand`) — white-label with no fork.
- **Adoption docs** — build-your-first-product tutorial, extending guide, architecture, why-govern-ai.
- **Apache-2.0** licensed; NOTICE credits the authors.

_On release, move this block out of PENDING and date it (see RELEASE.md)._

[Unreleased]: https://github.com/potkei/mantlekeep/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/potkei/mantlekeep/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/potkei/mantlekeep/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/potkei/mantlekeep/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/potkei/mantlekeep/releases/tag/v0.1.0
