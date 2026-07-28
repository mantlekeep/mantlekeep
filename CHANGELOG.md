# Changelog

All notable changes to MantleKeep are documented here.
Format: [Keep a Changelog](https://keepachangelog.com); versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.0] — Unreleased

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

[Unreleased]: about:blank
[0.1.0]: about:blank
