# MantleKeep

**MantleKeep is a governance framework for human and AI actions.** Products are built
on it and extend it through dependency injection and adapters; MantleKeep owns the
*shape* of governance, not the runtime. It rides mature runtimes (Spring/Reactor on
the JVM, Go on the control plane) rather than rebuilding them — it adds only the
governance primitives: the one **door**, the tamper-evident **hash-chain**, the
**ports/adapters** seam, and the **sealed floor**.

> MantleKeep : Spring :: Spring : the JVM — a framework in its own lane, standing on
> the layer below.

The one rule the whole design serves: **govern before you execute.** Every action —
human or AI — is submitted to one door, decided against policy, and recorded on an
append-only hash-chain *before* any side effect runs. A deny aborts before anything
happens.

This repository is the **surface** of the framework: the generic engine, the SDK and
port contracts, example adapters, and a runnable tutorial. It is enough to adopt and
extend MantleKeep. It is licensed under **Apache-2.0**.

---

## What's in here

| Path | What it is |
|------|------------|
| `mantlekeep-control/` | The **core** — a generic Go governance engine. The one door, the hash-chain audit, the policy/RBAC resolver, the orchestrator/saga spine, and the port interfaces. Knows zero product, action, environment, or role names. |
| `sdks/java/` | The **Java SDK** — a config-driven door client (`MantlekeepClient`), the adapter SPI, an in-memory store adapter, an allow-list policy adapter, and a Spring Boot starter. The socket products plug into. |
| `mantlekeep-spring-boot/` | The **Spring Boot starter family** — reactive (WebFlux) door client, the `@MantlekeepIntent` aspect, identity resolution, and an AI/agent starter (BYOK adapter behind a port). |
| `docs/build-your-first-product.md`, `docs/examples/FirstProduct.java` | The **worked example** — a runnable tutorial that builds a governed product against the SDK (write a `WorkerPort`, wire the door, submit intents). This is MantleKeep's example. |
| `docs/` | Usage notes and adoption guides — see **Documentation** below. |

**Architecture in one line:** does a piece *decide* (→ control plane, embeds the core)
or *execute* (→ a service, calls the door)? The core knows only the **port**; backend
knowledge lives in the **adapter** (`WorkerPort`, `AgentPort`, `StorePort`,
`PolicyEvaluator`, …). Swap a backend by configuration, not by rewriting the core.

## Documentation

Start here to adopt and build on MantleKeep:

| Doc | What it gives you |
|-----|-------------------|
| [Why govern human + AI actions](docs/why-govern-ai.md) | The case for adopting: the problem, MantleKeep's answer, who it's for. |
| [Architecture](docs/architecture.md) | One page: the decide-vs-execute rule, the core, the ports/adapters, the dependency direction — with a diagram and a full text description. |
| [Build your first governed product](docs/build-your-first-product.md) | A runnable ~5-minute tutorial: write a `WorkerPort` adapter, wire the door, submit intents, and see govern-before-execute + the hash-chain. |
| [Extending MantleKeep](docs/extending.md) | The adapter guide: the ports, the SPI + `ServiceLoader` discovery, adding an adapter, and how config selects it. |
| [The door — library + wire contract](docs/door.md) · [Java SDK quick-start](docs/sdk-quickstart.md) | Reference notes on the core and the SDK. |

## Build & run

### Core (Go)

Requires Go 1.26+.

```bash
git clone https://github.com/potkei/mantlekeep.git
cd mantlekeep

# From the repo root (a go.work covers the module):
go build -o mantlekeep ./mantlekeep-control/cmd/mantlekeep
go test ./mantlekeep-control/...

# Or from inside the module — it is a standalone Go module too:
cd mantlekeep-control
go build -o mantlekeep ./cmd/mantlekeep
go test ./...

# Run the door + spine smoke demo (embedded, no external services):
go run ./cmd/mantlekeep
```

On Windows use `.\` and an `.exe` name — `go build -o mantlekeep.exe .\cmd\mantlekeep`.

Two rules worth knowing: pass the **package path** (`./cmd/mantlekeep`), not just `-o`; and
from the repo root, patterns must name the module (`./mantlekeep-control/...`) — a bare
`./...` at the root matches no module and fails.

The smoke demo submits a batch of intents to the door, prints each verdict, and
verifies the audit hash-chain is intact — then runs the orchestrator spine, including
a saga rollback with compensation.

### Java SDK

Requires a JDK 25 toolchain. **Build with either Gradle or Maven** — both are first-class and
produce the identical `dev.mantlekeep:*:0.1.0` artifacts.

```bash
cd sdks/java
./gradlew build          # Gradle
mvn install              # Maven — same coordinates, same tests
```

Consume the whole governance stack with one dependency:

```gradle
implementation "dev.mantlekeep:mantlekeep-spring-boot-starter:0.1.0"
```
```xml
<dependency>
  <groupId>dev.mantlekeep</groupId>
  <artifactId>mantlekeep-spring-boot-starter</artifactId>
  <version>0.1.0</version>
</dependency>
```

### Spring Boot starters

The starter family (`-core`, `-starter-webflux`, `-starter-ai`) also builds with either tool:

```bash
cd mantlekeep-spring-boot
./gradlew build          # Gradle
mvn install              # Maven
```

## Configuration namespace

Everything is config-driven. The conventions:

- Environment variables: `MANTLEKEEP_*` (e.g. `MANTLEKEEP_DOOR_URL`, `MANTLEKEEP_BIND_STORE_AUDIT`).
- Spring properties: `mantlekeep.*` (e.g. `mantlekeep.door.url`, `mantlekeep.brand`).
- Identity headers at a service boundary: `X-Mantlekeep-User`, `X-Mantlekeep-Roles`.
- Go module path: `mantlekeep.dev/control`. Java packages: `dev.mantlekeep.*`.

## White-labelling — ship it under your own name, without forking

Your organisation can ship a branded binary whose operators never type `MANTLEKEEP_`.
A worked example is in the repo — build it and run it:

```bash
cd mantlekeep-control
go build -o acme-govern ./cmd/acme-govern
ACME_BRAND_NAME="Northwind Governance" ./acme-govern
```

It links **no extra engine code**: the same door, the same policy floor, the same
hash-chained audit as the stock `mantlekeep` binary. Only two things differ, both
configuration:

- `app.RemapEnvPrefix("ACME", "MANTLEKEEP")` copies every `ACME_*` variable onto the
  name the engine reads — one call covers present and future variables, because it
  works by prefix, not an enumerated list.
- Four brand defaults, each still overridable from the environment.

To make it yours: copy `cmd/acme-govern/`, change the `brandPrefix` constant and the
four defaults. That is the whole procedure.

**Do not fork the framework to rebrand it.** Renaming its packages or copying its
source takes you off the upgrade path — a security fix then has to be re-applied by
hand, forever. The seam exists so the engine can stay an unmodified dependency: your
name on the outside, one shared engine underneath.

## License

Apache-2.0. Copyright 2026 Kelvin Chan and the MantleKeep co-owners. See `LICENSE`
and `NOTICE`.
