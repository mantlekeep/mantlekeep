# MantleKeep roadmap

Living document. Read first when you return; update it when something lands.
Last updated: 2026-08-01.

**North star:** promote `0.1.0-rc.1` → `0.1.0` final. The gate is fixed:
the API has held for a full candidate cycle · every documented command runs from a fresh clone · a real
consumer has driven it end to end · the scanners are clean · no known defect in the surface it claims.

Everything below is the path to that gate, ordered so each step unblocks the next.

---

## Now — consolidate before adding

**1. Cut and publish the accumulated candidate.** `0.1.1-rc.1` closes the gaps a real consumer's build
surfaced (identity reaching the door, nested floor parameters, brandable policy namespace and headers,
`GovernedWorker`). See `CHANGELOG.md`.

**2. Consolidate the two Java module trees.** *This is the multiplier.* `DoorClient`, `Intent`, and door
config are declared twice, so every door-contract change must be made twice and one copy drifts — the cause
of a run of defects. Target: the pure-JDK `door-client` is the one spine; each web framework is a thin
optional adapter (`docs/java-consolidation.md`). Doing this first means every step below lands **once**, and
it delivers the reactive path the features already on the SDK side (e.g. transition-level `runUnder`).

## Next — the maturity core (built once, on the consolidated spine)

**3. A governed saga runner in the Java SDK.** The single highest-value primitive. It dispatches every step
through `GovernedWorker` (a product cannot write a raw execute loop — the bypass hole closes by
construction), compensates in reverse (the engine the Go core already proves), and emits step evidence at a
configurable recording level (`docs/recording-levels.md`). One primitive: closes the bypass, delivers
step-level evidence, and stops every product reinventing the loop.

**4. Saga records + recording levels.** Persist step evidence through `StorePort` (per-purpose bindable — the
chain and the saga timeline can use different backends by config), correlated to the chain head so it is
tamper-evident without being on the chain. Recording verbosity (`none | decisions | steps | full`) is
env-scaled policy; govern-before-execute stays sealed at every level.

**5. A typed denial taxonomy.** Stable denial codes (`DENY_FLOOR`, `DENY_SEPARATION_OF_DUTIES`,
`DENY_IDENTITY`, …) so a product maps refusals to `400/401/403/409` deterministically and never surfaces a
`500`. Small, and it removes product-specific string parsing of deny reasons.

## Then — the promotion gate (rc → 1.0 credibility)

**6. SAST + supply-chain hardening.** Run the scanners and fix findings: `staticcheck` + `govulncheck` on
Go, SonarQube (or SpotBugs/PMD first) on Java. Add a **dependency-count CI gate** so the near-zero footprint
(one Go dependency, a zero-dependency Java spine) becomes an enforced, advertised property rather than a
claim. This *is* the promotion gate — "safest framework" as a verified label.

**7. A surface principles ruleset.** The SAST-safe subset of the engineering standard, in a form an AI
assistant loads automatically (`copilot-instructions` / equivalent) so products built on MantleKeep are
SAST-clean by default. The full standard stays private; the surface subset is the skill.

**8. Publish checklist** (gated on owning the `mantlekeep.dev` domain — one purchase unlocks the vanity Go
import, the Maven Central namespace, and the pretty module path together): javadoc jar, signing, POM
metadata, pkg.go.dev.

## Later — parked, sequenced

- **Reusable / warm-engine worker adapter** (node reuse, DinD, Dagger) — plugs into the saga runner without
  changing it; cost/flexibility without touching governance (`docs/reusable-worker.md`).
- **Atomicity & idempotency** — crash-safety at scale (idempotency keys on transition + step emit). Real,
  but not demo-blocking.
- **A structural control for governance** — credential brokering, so bypassing execution is impossible
  rather than merely visible (`docs/credential-brokering.md`).
- **Multi-zone / data residency** — federated doors, a broker per zone, hashes cross boundaries not records
  (`docs/multi-zone.md`).

---

## How the backlog is sourced

The Next-tier items are not invented — a real consumer building a governed delivery service against this
framework hit its edges, **failed closed rather than bypass governance**, and returned a gap report. Their
P0s are items 3–5. A framework whose backlog is written by a real regulated deployment, and whose consumer
chose to stay blocked rather than break the model, is the signal the release-candidate process exists to
produce.

## Design notes (the "why" behind the items)

`docs/behaviours.md` · `docs/layering.md` · `docs/recording-levels.md` · `docs/java-consolidation.md` ·
`docs/credential-brokering.md` · `docs/reusable-worker.md` · `docs/multi-zone.md` ·
`docs/audit-postgres-adapter.md`
