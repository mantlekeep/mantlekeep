# Changelog — mantlekeep-control

<!-- purpose -->
**mantlekeep-control — the governance engine.**

Every human and AI action goes through one door: it is validated, decided by policy, recorded on a
hash chain, and only then does anything execute. The core knows no product, no environment and no
role name of its own — those are DATA a deployment supplies, which is what lets one engine govern
estates, pipelines and agents without learning what any of them are.
<!-- /purpose -->

Versions here are this MODULE's own: it is tagged `mantlekeep-control/vX.Y.Z` and released independently of
everything else in the repository. Format: [Keep a Changelog](https://keepachangelog.com);
versioning: [SemVer](https://semver.org).

Releases before the modules were split are in the [repository CHANGELOG](../CHANGELOG.md) under
bare version numbers — one version described everything then.

## [Unreleased]

### Fixed — the SSO tier could not resolve anybody over HTTP

`doorserver` had no way to carry a caller's IdP groups. `resolveUser` called
`Identity.Resolve` with `ExternalIdentity{ID: userID}` and nothing else, and the submit path then
replaces the intent's subject with that result — so a group could not reach the resolver by any
route, header or body.

The gateway resolver (the SSO tier, `MANTLEKEEP_AUTH=proxy`) decides roles from a group→role
table and refuses an identity whose groups map to nothing. With no groups to map, it refused
**every** caller. Worse than a denial: the refusal happens at IDENTITY, before any policy
evaluation, so **the door records nothing at all** — an operator sees a 403 and goes looking for
a policy bug that does not exist.

Found by running the deployment shape end to end rather than by reading: a real gateway-resolver
door, a real estate in front of it, and every request failing with no decision logged.

### Added

- **`Options.TrustedGroupsHeader`** — names the header carrying the groups the fronting gateway
  asserts, comma-separated. Empty (the default) keeps the previous behaviour exactly, so this is
  additive for anyone already running the door.

  It carries **groups, never roles**, and that distinction is the control: a caller asserting its
  own roles would be asserting its own authority, while a group stays a claim the resolver
  interprets against MantleKeep's own configuration. The resolver remains the authority on what a
  group is worth.

  Validated by `checkIdentityHeader`, like the other identity headers: `Authorization`, `Cookie`
  and the rest are refused at construction, because the value reaches the decision record in an
  append-only chain where it cannot be redacted afterwards.

  Trust it on the same terms as `TrustedUserHeader` — only set it when something in front
  authenticates and strips any client-supplied copy.

## [v0.4.1] — 2026-09-08

A correctness and hardening release. No API change: the diff from v0.4.0 removes zero exported
declarations, so upgrading is a one-line change in `go.mod`.

Cut because v0.4.0 turned out to carry findings that a SonarQube profile stricter than this
project's own blocked a downstream release on. A module proxy tag can never be replaced, so the
answer to a tag with findings in it is the next tag.

### Fixed — the audit chain no longer defaults into the shared temp directory

Four files wrote there, including the SDK's DEFAULT audit chain and the kernel's scan input.

`os.MkdirTemp` creates an owner-only directory with an unguessable name, which defeats the symlink
and pre-create attacks. But the directory it creates that in is world-writable, shared with every
process on the host, and the first thing a reboot or a cleaner removes. A governance engine whose
answer to "where is the chain" is "/tmp, until something tidies it" has no chain.

They now write under the deployment's own data directory — 0750, owner and group only,
traversal-rejected, named by `MANTLEKEEP_DATA_DIR`.

The kernel's case is the sharpest. It wrote the scan input and then handed the PATH to the
sandbox, so anything on the host could act between the write and the read. An owner-only directory
removes that window rather than narrowing it.

**If you rely on the default audit path**, it has moved from a temporary directory to
`mantlekeep-data/` relative to the working directory. That is a behaviour change, and it is the
point: the previous default did not survive a reboot.

### Changed — long functions split, repeated messages named

`LoadFloors` gained `appendLayeredFloors` and `mergeFloors`; two test helpers were split. Nine
`if` statements that bound a variable used once in their own condition now put the expression in
the condition. `"policy grants: %w"` and `"policy floors: %w"` are named constants, so a reader
sees one origin for each message.

No behaviour changed with them — the same 18 packages pass.

## [v0.4.0] — 2026-09-08

Purely ADDITIVE. No exported symbol was removed or renamed, so a consumer on v0.3.0 upgrades by
changing one line in `go.mod` — asserted rather than assumed: the diff from v0.3.0 contains zero
removed exported declarations.

### Added — policy that changes without a restart

`doorkit.ReloadPolicy(ctx, grants.Loader)` re-reads the grant and floor documents and installs
them if they are valid. `doorkit.PolicyRevisionInForce()` reports what is actually deciding.

The whole snapshot is built BEFORE anything is installed, so a malformed document, an unreadable
file or a product doc violating the platform seal fails while the previous policy is still in
force. There is no window where the door runs on half a policy, and no path where a broken file
empties the grants — which would not look like an outage, it would look like a working deny-all.

Boot still fails fast and a reload never does: at boot nothing is serving, so a bad document means
the process must not start; during a reload something IS serving valid policy, and turning a typo
into an outage of the component that says no is the worse failure.

### Added — one way to ask what the door decided

`DecisionFrom(err) (Decision, bool)` and `AwaitingApproval(err) bool`.

Submit reports a non-allow outcome as an error, and two shapes carry a decision: `DecisionError`
(what the door returns) and `Refused` (narrower, still constructed by some callers). A caller
checking only one gets no compile error when it meets the other — it gets an assertion that
silently misses and reports a change WAITING FOR A PERSON as forbidden. That happened, was fixed,
and happened again in a second handler when the error type changed.

### Added — the policy READ surfaces

`PolicyInputFor`, `app.PolicyInForce` (a `grants.Loader` over what the ENGINE accepted, not the
file on disk), `app.Explainer` (what the door WOULD decide, recording nothing and issuing no
token), and `app.EvaluationOrder()` with `Decision.Step`.

`Decision.Step` names the stage that produced a decision, so a surface can locate a refusal in the
evaluation order without matching on the reason text — a second engine that breaks silently
whenever a message is reworded. It is separate from `Category`: category says what KIND of denial,
step says which question asked.

### Added — `policybundle`: one policy for the door and a gateway

Serves the documents in force as an OPA bundle, so a gateway evaluates LOCALLY against the same
documents the door enforces. It links no OPA — a bundle is a gzipped tar of JSON — and reads
through `grants.Loader`, so it publishes whatever source the deployment already uses.

Tagged with the revision derived from the documents, the SAME string the door reports, so "the
gateway and the door agree" is a comparison an operator can run rather than an assurance.

### Added — `grants.RevisionOfDocuments`

One canonical derivation, so every producer of a revision derives it identically. Two sources that
hash the same policy differently are not two opinions; they are a broken join.

### Note for consumers

`Decision` gained a field. Struct literals that name their fields are unaffected; an UNKEYED
literal (`Decision{a, b, c}`) will not compile. `go vet`'s composites check flags those already.

## [v0.3.0] — 2026-09-07

*Backfilled 2026-09-08. This tag shipped without a release note; the entry is reconstructed from
the diff and the commits, and says only what those support.*

### Added — a layer cascade that can TIGHTEN a grant

A configuration layer that names an action now decides it, where before a grant document decided
alone. The rule only ever REFUSES more: a case that gains an ability under the new rule is a
governance regression, not a fix. Exactly 3 of 60 live decisions changed when it landed, all
allow→deny, proven by building an engine on the old rule and reproducing the previous matrix
byte-identically.

### Added — a boot diagnostic that names what a layer decides

Startup reports which actions a layer now decides, and when a platform seal REJECTED a layer's
value. A cascade whose effect is invisible until somebody is refused is a cascade nobody can
review before it refuses them.

### Added — a directory that admits what it is

`DevSubjectsEnv` (`MANTLEKEEP_DEV_SUBJECTS`) lets a deployment name its own dev population. More
importantly, a directory that cannot be listed now SAYS SO rather than answering with an empty
list — an empty directory and an unreachable one send a person to entirely different places, and
reporting the second as the first is how a permissions screen lies confidently.

## [v0.2.0] — 2026-09-07

Go module only. The Java and Python SDKs are unchanged since `0.1.1` and are **not** re-released:
a version number should mean something changed, and for them nothing did.

**A minor bump because this breaks the Go API** — which under SemVer major-zero is the correct
signal. `0.x` still means the surface is not stable; `1.0.0` would claim a maturity nothing here
has yet earned.

### Why upgrade

- **`doorserver` refuses a credential header as the caller-identity header.** Pointed at
  `Authorization` or `Cookie`, the door would have written a live bearer token into the
  append-only hash chain — where it cannot be redacted without breaking the proof the record was
  not edited. Now refused at construction. This is the reason not to stay on `v0.1.3`.
- **`internal/audit` has tests**, which it did not before. It is the hash-chained evidence spine.
- Six functions split under the cognitive-complexity limit; `registry.Register`/`Ingest` take a
  `Registration` value instead of six consecutive strings, where transposing two compiled cleanly
  and stored the wrong thing under the right name.

### Breaking

See **Changed — BREAKING (Go API)** below for the migration table. Four exported single-method
interfaces are renamed for the method they declare. Renames only — no behaviour changes, no
signature changes beyond the names.

### Also in this tag

`mantlekeep-estate` now carries a `replace` directive for `mantlekeep-control`, so a clone of this
repository builds every module **from itself**. Without it, building the estate as a standalone
module downloaded a *different* control than the one sitting beside it — so an organisation that
clones, scans, and then builds one module per pipeline would have built against code the scan
never saw.


### Added
- **`mantlekeep-kafka` — the governed-grant adapter for Apache Kafka.** A new Go module, sibling to
  `mantlekeep-control`, that applies an **already approved** grant to a Kafka cluster through the
  Admin API. It decides nothing; the door decided before it was called.

  Two operations, and the **asymmetry is the design**:
  - **`OnboardTeam(boundary)`** — rare, gated. Gives a team a namespace it owns: **PREFIXED** ACLs
    over its prefix (TOPIC: `READ`/`WRITE`/`DESCRIBE`, GROUP: `READ`) plus a producer/consumer
    byte-rate **quota** for its principal. **`CREATE` is deliberately not granted** — the team may
    read and write everything under its prefix and still cannot bring a topic into existence, so
    topic creation stays a governed act. PREFIXED rather than LITERAL because a literal ACL per
    topic recreates ACL sprawl and turns every playground topic into a governance event; a golden
    path slower than the bypass stops being used.
  - **`Provision(grant)`** — frequent, instant. Creates one topic inside a namespace the team
    already owns. It grants nothing new (the prefix ACL already covers it), refuses a name outside
    the granted prefix, and is idempotent: an existing topic is success, not failure.

  Every artifact is **read back** from the cluster (`DescribeACLs` / `DescribeClientQuotas` /
  `DescribeTopicConfigs`), never echoed from the request — a result reported from its own input is
  testimony, not evidence. Limits (quota, retention, partitions, replication factor) are **inputs**;
  the adapter invents none of them.

- **The workspace now holds one module per dependency tree**, with the rule written into `go.work`:
  the core links only bbolt, and each adapter carries its own heavy client. `mantlekeep-kafka`
  depends on the core; the core depends on no adapter. A CVE or a registry quarantine in the Kafka
  tree therefore cannot block the engine's build — proven, not asserted: the dependency guard runs
  per module in CI and the core's budget stays at **1**.

- **CI and security gates extended to the new module** — `go vet` · staticcheck · govulncheck · test
  in `ci.yml`, and gosec · staticcheck · govulncheck in `security.yml`, each scanning the adapter's
  dependency tree separately from the engine's. **No broker is required to run the tests**: the
  cluster sits behind an `Admin` interface, so prefix refusal, ACL shape, quota shape and
  already-exists idempotency are all decided — and asserted — without one.

### Changed — BREAKING (Go API)

Three exported single-method interfaces (and one internal) are renamed for the method they
declare, the Go convention
(`Reader`/`Writer`/`CloseNotifier`); a name that shares nothing with its method makes a reader
open the type to find out what implementing it costs. Renames only — **no behaviour changes**,
and no signature changes beyond the names themselves.

| Was | Now | Why |
| --- | --- | --- |
| `mantlekeep.WorkflowEngine` | `mantlekeep.WorkflowRunner` | its method is `Run` |
| `extension.RouteRegistrar` | `extension.Router` | see below — the method is renamed too |
| `registry.Source` | `registry.Fetcher` | its method is `Fetch` |
| `registry.GitFetcher` | `registry.GitCloner` | frees the `Fetcher` name for the interface above; this is the narrower git-clone port injected into `GitSource`, not the registry's ingestion port |
| `registry.GitSource{Fetcher: …}` | `registry.GitSource{Clone: …}` | the field holds a `GitCloner` |

`registry.Register` and `registry.Ingest` now take a `registry.Registration` value instead of a
positional parameter list:

```go
// was
r.Register(ctx, "scan-tool", "tool", "Scanner", "alice", "1.0.0", "sha256:aaa", nil)
// now
r.Register(ctx, registry.Registration{
    Name: "scan-tool", Kind: "tool", Title: "Scanner",
    Owner: "alice", Version: "1.0.0", Ref: "sha256:aaa",
})
```

`Register` took six consecutive strings — title, owner, version and ref among them — so
transposing any two of them compiled cleanly and stored the wrong thing under the right name.
`Ingest` took nine parameters in total. `Ingest` still supplies `Ref` itself (the digest of the
bytes that actually arrived) and still falls back to the source's descriptor for an empty
`Manifest`.

`extension.Router`'s method is renamed `Handle` → `Route`. The value REGISTERS a handler
against a pattern; it does not serve the request. `Handle` invited the reader to expect an
`http.Handler`, which is the one thing it is not.

**To adopt:** rename at the call sites. An implementor of `extension.RouteRegistrar` renames its
`Handle` method to `Route`; nothing else changes shape. `internal/policy`'s `Source`/`SourceFunc`
became `Loader`/`LoaderFunc` in the same pass but are internal, so no consumer sees them.

### Security

- **`doorserver.New` refuses a credential-bearing header as the caller-identity header.**
  `TrustedUserHeader` / `DelegatedSubjectHeader` set to `Authorization`, `Proxy-Authorization`,
  `Cookie`, `Set-Cookie`, `X-Api-Key`, `Api-Key` or `X-Auth-Token` is now rejected at
  construction (case-insensitive, whitespace-trimmed) rather than accepted.

  The door records the caller's id as the SUBJECT of every decision — in the console decision
  log and in the **hash-chained audit record**. Pointing the identity header at a credential
  header made the door write a live bearer token there as a user id, into an append-only,
  tamper-evident log where it cannot be redacted afterwards without breaking the chain that
  proves the record was not edited. A one-character configuration slip with a permanent
  consequence, so it is now a machine-enforced refusal instead of a documented caution.

  **To adopt:** only a deployment that was already recording credentials as user ids is
  affected, and it fails fast at startup with a message naming what to set instead.

### Removed

- **`internal/policy.liveSnapshot`** — it declared `RequiredRole(string) (Role, bool)`, which is
  exactly `ActionAuthorizer`, in the same file. Two names for one contract let the two drift.
  `WithLive` now takes `ActionAuthorizer` directly; internal, so no consumer sees it.

### Added

- **`internal/audit` is tested.** The bbolt hash-chained audit log — the framework's evidence
  spine — had no tests. Now covered: the chain link between records, an intact walk, a record
  edited behind the logger's back, an unreadable record, `Count` on an empty vs populated chain,
  and `Records`' newest-first order and limit.
- **`var _ mantlekeep.WorkflowRunner = (*orchestrator.Engine)(nil)`** — the doc comment claimed the
  Engine implements the core contract; now the compiler checks it.

## [v0.1.3] — 2026-09-06

*Backfilled 2026-09-08. Cut so `mantlekeep-estate` could be published against a real tag rather
than a `replace`.*

### Added — the two things a gated change needs

- **`Refused`** — a TYPED refusal carrying the action, the reason, and who may sign off. An error
  string alone loses the distinction that matters most: "deny" is final, while a
  `require_approval` is a change WAITING for a person. A caller that cannot tell them apart
  reports a pending approval as a failure, which is how a governed change looks broken to whoever
  submitted it.
- **`require_approval_when`** — the floor rule kind that makes gating reachable at all. Without
  it the approval machinery was complete and permanently unreachable, which is the defect a live
  cluster surfaced the night before.

Both were additive, and both existed because `mantlekeep-estate` could not be published without
them: a tag whose consumer cannot compile is a tag that has to be replaced, and module proxy tags
are cached permanently and can never be replaced.
