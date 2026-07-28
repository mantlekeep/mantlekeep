# mantlekeep-spring-boot

A Spring Boot SDK that gives an application a **governed** link to the MantleKeep door: every
governed action is submitted to the one door (`/api/govern`), decided by policy, and recorded
on the hash-chain. The app never re-implements governance — it declares intents.

- **Java 25 · Spring Boot 4.1 · reactive (WebFlux).**
- One dependency → an auto-configured `DoorClient`, a declarative `@MantleKeepIntent` annotation,
  and an `AgentPort` seam for BYOK model/agent adapters.

## Modules

A **starter family** — a product picks the stack it needs:

| Module | Purpose |
|---|---|
| `mantlekeep-spring-boot-core` | shared, transport-agnostic contracts (reactor-only) |
| `mantlekeep-spring-boot-starter-webflux` | reactive `DoorClient` + `@MantleKeepIntent` aspect + auto-config |
| `mantlekeep-spring-boot-starter-mvc` | *(reserved)* blocking variant |
| `mantlekeep-spring-boot-starter-ai` | *(reserved)* Spring AI → `AgentPort` |

## Install (Gradle)

```kotlin
dependencies {
    implementation("dev.mantlekeep:mantlekeep-spring-boot-starter-webflux")
}
```
> Version comes from your platform/BOM; published to your registry. No
> lockfile committed — build once, promote the artifact, config per env (config-layer model).

## Configure

```yaml
mantle:
  door:
    base-url: http://localhost:8080     # the core's door
    govern-path: /api/govern
    bearer-token: ${MANTLEKEEP_DOOR_TOKEN:}  # identity is owned by the SSO gateway; this is transport auth
    connect-timeout: 3s
    response-timeout: 10s
```
Every bean is `@ConditionalOnMissingBean` — override any piece (a custom `DoorClient`, `WebClient`, etc.).

## Use it — two ways

### 1. Declarative: `@MantleKeepIntent` (recommended)

Annotate a reactive method; the aspect submits the intent **before** the body runs, and the
body executes **only if the door allows**. On a denial the returned `Mono`/`Flux` errors with a
`DoorException` and the side effects never happen.

```java
@Service
class ReleaseService {

    @MantleKeepIntent(value = "job.promote", resource = "project/demo", goal = "promote the release to prod")
    public Mono<Release> promote(String version) {
        // runs only if the door allowed; otherwise never subscribed
        return releases.promote(version);
    }
}
```
> WebFlux-starter contract: annotated methods return `Mono` or `Flux`.

### 2. Programmatic: inject `DoorClient`

```java
@Service
class ReleaseService {
    private final DoorClient door;
    ReleaseService(DoorClient door) { this.door = door; }

    public Mono<Release> promote(String version) {
        Intent intent = Intent.of("job.promote")
                .resource("project/demo")
                .goal("promote the release to prod")
                .build();
        return door.submit(intent)                 // errors with DoorException on deny
                .then(releases.promote(version));
    }
}
```

## The agent seam (`AgentPort`, BYOK)

Whatever drafts an AI step — your Claude Code, Spring AI, or a local model — implements
`AgentPort`. The loop asks it to draft; the door governs the result. Model choice is an
adapter concern, never hardcoded.

```java
public interface AgentPort {
    Mono<String> draft(Role role, LoopContext context);
    Flux<String> draftStream(Role role, LoopContext context); // default: single-element stream
}
```

## Build

```bash
./gradlew build   # compiles + runs the tests on the Java 25 toolchain
```
The Gradle wrapper (9.6.1) is committed — no local Gradle needed.

## Design notes

- **`Decision` fails closed** — an absent verdict reads as `DENY`.
- **Identity is not in the intent body** — it flows from the authenticated caller at the door
  (SSO assertion), never trusted from the request.
- **Dedicated, timeout-bounded `WebClient`** for the door, isolated from the app's own clients.
- Contracts live in `-core` (framework-free, reactor-only); Spring wiring lives in the starter.
