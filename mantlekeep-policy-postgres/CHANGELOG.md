# Changelog — mantlekeep-policy-postgres

<!-- purpose -->
**mantlekeep-policy-postgres — holds the governance policy in Postgres instead of in a file.**

It implements the two ports the core defines: `grants.Loader`, so every replica reads the policy in
force from ONE place, and `grants.Writer`, so `grants.Govern` can apply a change the door has
already allowed. It is an ADAPTER and decides nothing — by the time Write runs, the decision
exists.

A file source is read by every replica and written by none, so policy changes arrive as a deploy.
That is the right answer for a deployment whose policy is a release artefact. It is the wrong one
for a deployment that edits policy through a UI, where a change must take effect without a
rollout, every replica must see it at the same moment, and somebody must be able to ask what the
policy said last Tuesday.
<!-- /purpose -->

Versions here are this MODULE's own: it is tagged `mantlekeep-policy-postgres/vX.Y.Z` and released
independently. Format: [Keep a Changelog](https://keepachangelog.com); versioning:
[SemVer](https://semver.org).

## [v0.1.0] — 2026-09-08

The first release. It implements the two ports the core defines, so a deployment can hold its
policy in Postgres without the core learning that Postgres exists.

### Added

- **`grants.Loader`** over a Postgres-held policy, so every replica reads one document set.
- **`grants.Writer`**, so a change the door allowed is applied — and only then.
- **Optimistic concurrency inside the store.** The change is computed INSIDE `Update` rather than
  handed to it: split across a read and a write there is a window in which another operator
  commits, and the second writer then stores a document computed from a policy that no longer
  exists — the first operator's change silently reverted while both are told theirs was applied.
- **A revision derived from the DOCUMENTS**, never from a row id, a sequence or a timestamp. That
  is what lets a file deployment and a database deployment serving identical policy agree, and it
  is what makes migrating between them provable rather than assumed.
- **`Seed`**, which creates a policy where there is none and can never change one that exists.
  It is the one write that does not go through the door, because at the moment it runs there is no
  policy for a door to consult — so it is confined to exactly that case. A bootstrap that could
  also overwrite would be the easiest way in the whole system to grant yourself a role.

### Refuses, on purpose

- **A failed read is never an empty policy.** Empty grants deny everything, so a source that failed
  and one that legitimately grants nothing would be indistinguishable — and the first would look
  like a working deny-all rather than an outage.

### Fixed — the declared mantlekeep-control pin is the one this module is tested against

`go.mod` declared `mantlekeep-control v0.2.0` while every build and test ran against the copy
beside it in the repository, because **Go ignores a `replace` in a dependency's `go.mod`** — only
the main module's applies. A consumer therefore resolved v0.2.0, not the tree under test. The pin
is now `mantlekeep-control v0.4.1`, and `scripts/check-release-pins.sh` fails any release where a
declared sibling pin is not that sibling's newest released tag.
