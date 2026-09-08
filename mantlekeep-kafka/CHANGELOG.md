# Changelog — mantlekeep-kafka

<!-- purpose -->
**mantlekeep-kafka — applies an approved grant to Kafka.**

The adapter that carries out what the door has already allowed, through the Kafka Admin API. It
decides nothing: by the time it runs, the decision exists and is on the chain. It lives in its own
module so a deployment that governs no Kafka links no Kafka client.
<!-- /purpose -->

Versions here are this MODULE's own: it is tagged `mantlekeep-kafka/vX.Y.Z` and released independently of
everything else in the repository. Format: [Keep a Changelog](https://keepachangelog.com);
versioning: [SemVer](https://semver.org).

Releases before the modules were split are in the [repository CHANGELOG](../CHANGELOG.md) under
bare version numbers — one version described everything then.

## [v0.1.1] — 2026-09-08

No API change. Cut so a repository-wide scan of this module is clean: v0.1.0 carried findings that
a SonarQube profile stricter than this project's own blocks a release on, and a module proxy tag
can never be replaced.

### Changed

Three `if` statements in the boundary tests bound a variable used once in their own condition.
`if bad.Validate() == nil` is the claim; the name added nothing the call did not already say.

Six functions over the cognitive-complexity limit were split in an earlier commit on this line.

No behaviour changed — the same two packages pass.

### Fixed — the declared mantlekeep-control pin is the one this module is tested against

`go.mod` declared `mantlekeep-control v0.2.0` while every build and test ran against the copy
beside it in the repository, because **Go ignores a `replace` in a dependency's `go.mod`** — only
the main module's applies. A consumer therefore resolved v0.2.0, not the tree under test. The pin
is now `mantlekeep-control v0.4.1`, and `scripts/check-release-pins.sh` fails any release where a
declared sibling pin is not that sibling's newest released tag.

## [v0.1.0] — 2026-08-30

*Backfilled 2026-09-08.*

### Added

A governed-grant adapter for Apache Kafka: the estate declares a topic, the door decides, and this
module is what carries out what was approved. It holds the BACKEND knowledge so the core does not
— the core knows only the port, and swapping Kafka for something else is a wiring change rather
than an edit to the engine.

Published as its own module so a deployment that governs no Kafka links no Kafka client, and a CVE
in that client cannot block a core build.
