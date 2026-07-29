# Changelog

All notable changes to MantleKeep are documented here.
Format: [Keep a Changelog](https://keepachangelog.com); versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.1] — Unreleased

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

[Unreleased]: https://github.com/potkei/mantlekeep/compare/v0.1.0...HEAD
[0.1.1]: https://github.com/potkei/mantlekeep/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/potkei/mantlekeep/releases/tag/v0.1.0
