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

## [v0.1.0] — 2026-08-30

*Backfilled 2026-09-08.*

### Added

A governed-grant adapter for Apache Kafka: the estate declares a topic, the door decides, and this
module is what carries out what was approved. It holds the BACKEND knowledge so the core does not
— the core knows only the port, and swapping Kafka for something else is a wiring change rather
than an edit to the engine.

Published as its own module so a deployment that governs no Kafka links no Kafka client, and a CVE
in that client cannot block a core build.
