# MantleKeep roadmap

Living document. Read first when you return; update it when something lands.
Last updated: 2026-08-01.

**North star:** promote `0.1.0-rc.1` → `0.1.0` final. The gate is fixed:
the API has held for a full candidate cycle · every documented command runs from a fresh clone · a real
consumer has driven it end to end · the scanners are clean · no known defect in the surface it claims.

Everything below is the path to that gate, ordered so each step unblocks the next.

---

## Now — consolidate before adding

**1. Cut and publish the accumulated candidate.** `0.1.0-rc.2` closes the gaps a real consumer's build
surfaced (identity reaching the door, nested floor parameters, brandable policy namespace and headers,
`GovernedWorker`). See `CHANGELOG.md`.

**2. Unify the `Decision` up, and consolidate onto it.** *These turned out to be one job.* The two Java
trees do not merely duplicate the door types — they encode two different contracts: a minimal 2-state
`Decision` (allow/deny) in the SDK, and a rich 3-state one (`ALLOW | DENY | REQUIRE_APPROVAL`, with
`policyId`, `reasons[]`, `requiredApprovers`, `expiresAt`) in the starter core — which matches what the Go
engine already decides but never sends. Consolidating *down* to the minimal shape would delete the
approval structure a governed propose→approve flow needs. So: enrich the wire and the SDK `Decision` to the
rich 3-state shape (the engine already models it), fold in the typed denial taxonomy (item 5) as the
`outcome` + coded `reasons`, then unify both clients on the one type. Delivers consolidation, typed denials,
and the reactive `runUnder` path in a single move. Original consolidation note: `docs/java-consolidation.md`.

**2b. (folded into 2)** The old "consolidate the two trees" — *This is the multiplier.* `DoorClient`, `Intent`, and door
config are declared twice, so every door-contract change must be made twice and one copy drifts — the cause
of a run of defects. Target: the pure-JDK `door-client` is the one spine; each web framework is a thin
optional adapter (`docs/java-consolidation.md`). Doing this first means every step below lands **once**, and
it delivers the reactive path the features already on the SDK side (e.g. transition-level `runUnder`).

## Next — the maturity core (built once, on the consolidated spine)

**3. Dogfood: reference products drive the framework — the primary integration test.** Build real governed
services on MantleKeep (a delivery pipeline, a workload-session lifecycle, a registry crossing) and run them
against the SDK the way any consumer would. This is not a nice-to-have: **unit tests cannot catch a defect
in the seam between components — only a consuming product can**, and every integration bug found so far was
found exactly this way. A reference product that depends on MantleKeep is the cheapest, most controllable
real consumer there is; it catches a class of bug before any downstream consumer meets it, and it is a
second independent witness for the promotion gate ("a real consumer has driven it end to end"). Do this
right after consolidation, so the products import the final pure-JDK spine once.

**4. A governed saga runner in the Java SDK.** The single highest-value primitive. It dispatches every step
through `GovernedWorker` (a product cannot write a raw execute loop — the bypass hole closes by
construction), compensates in reverse (the engine the Go core already proves), and emits step evidence at a
configurable recording level (`docs/recording-levels.md`). One primitive: closes the bypass, delivers
step-level evidence, and stops every product reinventing the loop.

**5. Saga records + recording levels.** Persist step evidence through `StorePort` (per-purpose bindable — the
chain and the saga timeline can use different backends by config), correlated to the chain head so it is
tamper-evident without being on the chain. Recording verbosity (`none | decisions | steps | full`) is
env-scaled policy; govern-before-execute stays sealed at every level.

**6. Typed denial taxonomy — FOLDED INTO ITEM 2.** (Unifying the `Decision` up carries the typed `outcome` + coded reasons; kept here for the record.) Stable denial codes (`DENY_FLOOR`, `DENY_SEPARATION_OF_DUTIES`,
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
- **Multi-language SDK generation from one source of truth.** Hand-written per-language clients are the
  two-stacks drift problem at language scale — the Java and Python clients already encode the same contract
  and the same behaviour twice, and nothing compares them across languages. Converge before a third client
  is hand-written. The direction: a single schema (proto used as an IDL only) drives a generator that emits
  **zero-dependency stdlib clients** — JSON over HTTP, no protobuf/gRPC runtime in the consuming language —
  so the near-zero-dependency guarantee holds for every SDK. Wire types are generated; governance behaviour
  (fail-closed, `require_approval` is not allow, decide-then-dispatch) lives in **one place** — a Rust core
  wrapped by FFI, or server-side with thin clients — never re-implemented per language, because a divergence
  there is a security divergence. Backward-compatibility is enforced by additive-only fields plus a
  schema-diff test in CI.

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
