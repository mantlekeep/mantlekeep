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
