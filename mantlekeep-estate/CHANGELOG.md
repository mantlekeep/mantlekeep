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

## [Unreleased]

### Added — a floor may PREFER clusters for a named app, in order

`AppRule.Prefer` is the platform's ordered preference — plan A, then plan B — applied by the new
`Placer.PlaceWith(claim, current, prefer)`. `Place` is unchanged and now calls it with no
preference, so existing behaviour is identical.

`Place` is three ordered steps and says why: *"legality, then stickiness, then capacity. The order
IS the guarantee."* A preference is inserted between stickiness and capacity, which leaves both
guarantees above it intact:

- **Legality still filters first and alone.** The preference is applied to clusters that are
  ALREADY legal, so naming one can never place data in the wrong jurisdiction, nor into a cluster
  the platform cannot see. A pin that could reach past residency would be the most direct route
  out of the rule residency exists to be.
- **Stickiness still comes next.** An app already running somewhere legal stays there, so editing
  this list does not migrate live workloads on the next reconcile pass. Moving a placed app
  remains a new governed decision.

Below those, a preference beats capacity — that is the point of expressing one. A preferred
cluster below the capacity floor is skipped, because the whole reason for plan B is that plan A
can be full.

**A fallback is never silent.** When no preferred cluster can take the work, placement falls
through to the capacity choice and the decision's reason says so:

```
no preferred cluster (uk-app-1, uk-app-2) could take this workload — emptiest of 3 legal clusters
```

That reason travels with the change and reaches the chain, so a platform team that named a cluster
and did not get it can read why rather than discovering it from a dashboard.

It lives on the FLOOR and not on a manifest deliberately: the team never names a cluster — it
declares environment, purpose and residency, and the platform chooses. `Placement` is parsed from
the team's own document, so a preference field there would let a team choose its own placement,
which is the thing the design removed. Keys are team-qualified like the gate rules.

The field is named `prefer`, not `clusters`. A list under the second name reads equally as
"deploy to all of these", and the two are opposite instructions — one selects a single cluster,
the other fans out. Multi-target deployment will be its own field, so neither can be mistaken for
the other by somebody reading a document at 3am.

Additive. A floor with no `prefer` on a rule behaves exactly as before.

### Added — a named app may be gated harder than its tier

`Floor.Apps` raises the gate for individual applications, keyed **team-qualified**
(`"payments/settlement-engine"`), with `Floor.GateForApp(tier, name)` resolving it.

`GateFor` is deliberate about tier being the only input — *"a prod Kafka topic and a prod database
cost the same attention because the blast radius, not the technology, is what is being governed."*
That is right about assets and silent about apps. An individual application can carry consequence
its tier does not describe — a payments engine in a shared environment, a system a regulator has
named — and a platform team needs to say "this one always waits for a person" without moving every
app in that tier.

**RAISE ONLY.** A rule may make an app cost more attention and can never make it cost less. The
same seam as `Floor.Gates`: *config chooses the policy and may raise a gate, but it cannot lower
the floor.* A rule that EXEMPTED an app from approval would be the most attractive line in the
document to anyone wanting a change waved through, and a bypassed guardrail governs nothing.

Enforced **twice**, deliberately:

- `GateForApp` returns the stronger of the tier's gate and the rule's, so a weaker rule cannot
  lower anything no matter what a document says — and the document is editable while the server
  runs.
- `validateApps` REFUSES a weaker rule at load, because a silently-ignored rule is worse than a
  refused one: an operator writes `"gate": "none"`, the file loads, and they believe they have
  exempted something. They would find out from an approval queue rather than from an error.

Keys must be team-qualified and a bare name is refused, because app names repeat across an
organisation — two teams both have a `checkout`, and an unqualified rule would reach a team nobody
told. An unrecognised gate is refused at load and ignored at resolution, never assumed permissive.

A rule naming no gate is legitimate and changes nothing: `AppRule` is a struct so a later field —
a cluster preference, an extra approver — is an added field rather than a changed document for
every deployment already running one.

Additive. A floor with no `apps` table behaves exactly as before.

### Security — any authenticated caller could read any team's estate

`GET /api/estate/{team}` resolved the caller and then **discarded it**, using the team from the URL
unchecked. The handler authenticated (*are you someone?*) and never authorised (*may you see this
team?*), so any value in `X-Caller` returned any team's footprint.

What that disclosed was not only a manifest. The response carries the `drifts` array, where
`"kind":"absent","detail":"approved but absent"` names precisely which of a team's approved
resources **do not currently exist** — the gaps between what was signed off and what is running.
That is reconnaissance, not merely disclosure.

Demonstrated against the real handler over real HTTP before the fix: a caller owning `payments`
read `treasury` and received its Kafka topic names, its tier, and every absent resource in it. The
`api` package had no tests at all, which is how it shipped.

Reads now go through the manager, which submits an intent to the door — the same door, on the same
chain, as every write. The handler's own comment had stated the reasoning for not doing so: *"No
intent is submitted, because nothing happens as a result of a read and there is no decision to
record."* That holds for mutation and fails for disclosure; nothing *happens* on a read, something
is *revealed*.

A refusal answers **404, not 403**, with the same sentence as a team that does not exist. A 403
confirms there is something to be refused, and for an endpoint whose whole risk is disclosure the
existence of another team's estate is part of what is withheld.

The team is not compared against an attribute of the subject, because `mantlekeep.Subject`
deliberately carries no team — a caller that could assert its own scope could assert its way past
any gate. Who may read which team is a policy question, so the door is asked.

### Added

- `Manager.Footprint(ctx, actor, team)` — the governed read.
- `Manager.ReadFootprintsFrom(reader)` — supplies the read side to delegate to once a read is
  allowed. `Service` satisfies `FootprintReader`.
- `estate.read` — its own action, so a policy can grant reading without granting writing.
- `ErrReadRefused`, `ErrNoFootprintReader`.

### Deployment — action required

**`estate.read` must be granted, or every read is refused.** Like `estate.apply`, the door answers
`no role permits action estate.read` until a policy grants it. Grant it to the roles that may read
an estate, alongside `estate.apply`.

A `Manager` built without `ReadFootprintsFrom` refuses every read with `ErrNoFootprintReader`
rather than answering ungoverned. `serve` wires it; a composition root that builds its own manager
must add it. Failing closed is deliberate for a control whose absence is invisible — an
ungoverned read looks exactly like a working one.

### Added — the gateway's asserted groups now reach the door

`api.GroupsHeader` (`X-Caller-Groups`, matching the door's own `TrustedGroupsHeader` default) is
read onto `Subject.ADGroups`, and `doorclient` forwards it to the door.

Without it, a door on the SSO tier could resolve **nobody** through this service. That door decides
roles from a group→role table and refuses an identity whose groups map to nothing; `HeaderCallers`
returned `Subject{ID: name}` and dropped the groups, so nothing could ever map. The refusal landed
at IDENTITY, before any policy evaluation, so **the door recorded nothing** — an operator saw a
403 and went looking for a policy bug that did not exist.

Groups are not roles, and that is why forwarding them is safe: a caller may say which groups it is
in, and may never say what those groups are worth. The door remains the only thing that decides
what a group means, and `Roles` are still never taken from a request — asserted by a test that
offers `X-Caller-Roles: L0-SuperAdmin` and requires it to be ignored.

Requires a `mantlekeep-control` that supports `TrustedGroupsHeader`. Against an older door the
header is simply ignored, so this is additive in both directions.

Verified by running the bank's deployment shape end to end — proxy auth, AD groups, no dev
directory. Before: 404 with no door decision logged. After:

```
door decision outcome=allow action=estate.read subject=reader-rachel via=mantlekeep-estate
```

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
  sibling's newest released tag — including refusing a sibling that has no released tag at all,
  rather than skipping it. It also rebuilds each module with its `replace` lines stripped, which
  catches a declared graph that will not compile; note that this second check runs a module's OWN
  tests, and those may never touch the API a consumer uses. It is the pin check, not the rebuild,
  that catches the defect described here.

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
