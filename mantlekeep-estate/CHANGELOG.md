# Changelog — mantlekeep-estate

<!-- purpose -->
**mantlekeep-estate — governs what runs where.**

A team declares the shape of its estate; this resolves that against a floor it cannot lower, chooses
where it may run, gates what needs a person, and records every decision on the chain. The team never
names a cluster — it declares environment, purpose and residency, and the platform chooses.
<!-- /purpose -->

Versions here are this MODULE's own: it is tagged `mantlekeep-estate/vX.Y.Z` and released independently of
everything else in the repository. Format: [Keep a Changelog](https://keepachangelog.com);
versioning: [SemVer](https://semver.org).

Releases before the modules were split are in the [repository CHANGELOG](../CHANGELOG.md) under
bare version numbers — one version described everything then.

## [v0.3.1] — 2026-09-08

### Fixed — a consumer resolved an older mantlekeep-control than this module is tested against

`go.mod` declared `mantlekeep-control v0.2.0` while every build and test in this repository ran
against the copy of mantlekeep-control sitting beside it. Both statements were true at once
because the module also carries a `replace` for its siblings, and **Go ignores a `replace` in a
dependency's `go.mod`** — only the main module's applies. So the graph under test and the graph a
consumer resolved were two different graphs, and nothing compared them.

Nothing in v0.3.0 is broken by this on its own: v0.3.0 compiles and passes its full test suite
against v0.2.0. The cost lands on the consumer's side of the boundary. Taking this module alone
pinned mantlekeep-control back to v0.2.0, so code written against the current mantlekeep-control
API — `DecisionFrom`, which is how an approval decision is read off an error — failed to compile
in a project that had done nothing wrong.

- The declared pin is now `mantlekeep-control v0.4.1`: what a consumer resolves is what this
  module is built and tested against.
- `scripts/check-release-pins.sh` now refuses a release whose declared sibling pin is not that
  sibling's newest released tag, and re-runs each module's own tests with the `replace` lines
  stripped — the graph that ships is the graph that gets tested.

No API change: no exported declaration was added, changed or removed. A consumer already resolving
mantlekeep-control v0.4.1 for other reasons sees no difference at all.

## [v0.3.0] — 2026-09-08

### Added — two seams for a deployment the framework cannot see

Both are ADDITIVE and both are optional. No existing signature changed, no new dependency was
taken, and a deployment that configures neither behaves exactly as it did before — asserted, not
assumed: with no transform configured, what reaches the adapter is compared byte-for-byte against
what `Resolve` produces.

- **`Labels` — a deployment's own grouping vocabulary.** `labels` on a manifest and on an app,
  carried through to every resolved `DesiredItem`, so a consumer can group a resolved estate
  without rebuilding the resolver's naming rule. An app's effective labels are its team's plus
  its own: it may ADD a key, never restate one its team declared, or the estate would report an
  app under a heading its own team disclaims.

  A label is descriptive and nothing reads one to make a decision. That is why a label may not
  take the name of a field the engine governs: it would render beside the real value, look
  authoritative and govern nothing — and the day somebody wired it up, relabelling would become a
  way out of the thing that field protects. **The forbidden set is DERIVED** by reflection over
  the engine's own shapes rather than written down, so a field added tomorrow is reserved
  tomorrow; a hand-written list would be a second place to edit and would fall behind on the
  first change. Keys take the same narrow shape as every other name here, and values are bounded
  and carry no control characters — a label reaches a UI, a log line and an evidence record,
  where one line must stay one line.

- **`ChangeTransform` — a change may be transformed before it is governed.** An optional hook,
  wired with `Manager.TransformChangesWith`, that rewrites a change BEFORE it is submitted to the
  door, so what a person approves is what will actually be applied. Preparation done after the
  approval means a person signed a description and something else was produced from it
  afterwards.

  The ORDERING is the guarantee: transform, then govern, then apply. A change that arrives
  already transformed is not transformed twice — the approval path replays a stored change, and
  re-running the rewrite there would apply what it produces NOW under an approval given for what
  it produced THEN. That is held by structure (`Approve` calls the door directly) rather than by a
  flag, because a flag can be wrong. A transform that fails refuses the change before the door:
  nothing is submitted, nothing is recorded as pending, and no adapter is called. A transform may
  rewrite what a change is; it may never rewrite which change it is.

  The interface names no tool and never will. One deployment may render a template, another may
  ask a separate system to prepare the work — the framework says only that a change may be
  transformed, which is what makes it a port rather than an integration.

### Changed — BEHAVIOUR (policy precedence)

- **A config layer can now TIGHTEN a grant document.** When both a grant document and the
  resolved layer cascade name an action, the cascade decides. Grant is NECESSARY; the cascade is
  SUFFICIENT-TO-REFUSE. Previously the document was asked first and returned on the spot, so a
  scope file saying `{"actionRoles": {"service.deploy": "L1-Architect"}}` asserted nothing where a
  document already granted `service.deploy` to a consumer — the file was read, the layer loaded,
  the boot log named it, and every consumer still deployed. An operator got positive feedback for
  a control that governed nothing.

  **Nothing became more permissive.** The rule moved from `D ∨ (L ∧ H)` to `(L ∧ H) ∨ (¬L ∧ D)`;
  only `D ∧ L ∧ ¬H` moves, and it moves from allow to DENY. `TestNothingBecameMorePermissive`
  walks the whole cross-product of documents, layers and subjects against a literal transcription
  of the old code rather than trusting that paragraph.

  **What to check before upgrading:** any action that a layer names AND a grant document grants.
  Subjects holding a role the layer does not reach will start being refused. The boot diagnostic
  below prints exactly that list.

### Added

- **`policy.PrecedenceNotices`** — the boot diagnostic for the rule above. One line per action a
  loaded layer changes, naming the FILE and the ACTION and the roles that lose it. Wired at boot
  for the platform/team layers and for each per-scope layer; the hot-reload poll path stays
  silent. It reports the **resolved** cascade, not the layer's own value: where a sealed floor
  above rejected what the file asked for, the notice says both, because a diagnostic that quoted
  the file would announce a requirement the engine does not have.
- **`SubjectLister` and `DirectoryDescriber`** (with `DirectoryDescription`) — optional
  capabilities of an `IdentityResolver`, discovered by type assertion. A resolver that cannot
  enumerate its population says nothing, and a surface that finds nothing must SAY the directory
  cannot be listed rather than render an empty one: an empty user list on a permissions screen
  reads as "nobody has access". The gateway resolver deliberately does not implement
  `SubjectLister` — it holds a group→role table, never people.
- **`app.DevSubjectsEnv` (`MANTLEKEEP_DEV_SUBJECTS`)** — seeds the dev directory from
  `"id=Role,Role;id2=Role"` so a deployment can look at itself with its own people in it rather
  than the six names compiled into the binary. Set-but-empty is a hard startup error, not a quiet
  fall back to the demo set. The result still describes itself as ASSERTED, not of record.
- `ContractVersion` is `3.1.0` — additive ports, no signature changed.

### Fixed — the findings a stricter SonarQube blocks a release on

Three functions were well past the cognitive-complexity limit, each split along a seam that was
already there rather than by cutting them in half:

| | |
|---|---|
| `validateFloor` | 22 → 11, the app-runtime block became `validateAppFloor` |
| `Manifest.validate` | 21 → 12, who the manifest is for, then what it asks for |
| `serve.Run` | 21 → 14, the SIGHUP handler became `reloadFloor` + `reloadFleet` |
| `KSM.free` | 18 → 6, the metrics scan became `readMemory` |

`reloadFloor` and `reloadFleet` stay separate on purpose: one signal, two decisions. A bad
registry must not discard a good floor reload, and reporting them together would make an operator
guess which half failed.

`"config: %w"` and `"fleet: %w"` are named constants. Three `if` statements that bound a variable
used once in their own condition now put the expression in the condition.

No behaviour changed with any of it — the same six packages pass.

## [v0.2.0] — 2026-09-07

*Backfilled 2026-09-08.*

### Added — the estate verifies identity for itself

Token verification moved into its own module so the estate's own dependency scan stays clean, and
`PS256` is accepted alongside the algorithms already supported.

**The fence that matters:** the trusted-identity header is refused OFF LOOPBACK unless a
deployment explicitly chooses otherwise. Bound to an address other people can reach, a believed
header means anyone who can route to the port is anybody — which was proven with `curl`, not
argued. A deployment that supplies its own resolver needs no fence; one that relies on the header
must say out loud that something in front of it strips and re-sets that header.

### Fixed — a team that has declared nothing

Asking for the estate of a team with no manifest answered as though the team did not exist.
Declaring an EMPTY estate and never having declared one are different facts, and a reader acting
on the first would remove what the second still has.

## [v0.1.0] — 2026-09-06

*Backfilled 2026-09-08.*

First published release of the estate: a team declares what it needs; the platform decides
whether, where and under what limits, and every one of those decisions is on the hash chain.

### Added

- **manifest → floor → gate → placement → drift → promotion**, with the floor hot-reloadable on
  `SIGHUP`: a bad file keeps the good one serving, and every decision records the floor revision
  that made it.
- **A CLI** so the person writing a manifest can check it before a service sees one. It accepts
  YAML (including KYAML) and hands it to the SAME strict parser the service uses, so a mistyped
  key is named and a bad indent is reported with its line.
- **Branding** — environment names a deployment can make its own, and a role that reaches the
  chain.
- `web/` became `api/`: in a tree whose folders name layers, "web" reads as UI, and it serves
  JSON.

### Depends on

`mantlekeep-control` as a published module, with the local `replace` deleted — so what is
released is what a consumer gets.
