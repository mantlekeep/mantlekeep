# mantlekeep-java — the config-driven MantleKeep SDK for Java

A product adds **one starter dependency + a few `mantlekeep.door.*` properties** and gets
governance — the one door, govern-before-execute, the hash-chained audit — writing
**ZERO wiring code**. ONE client, TWO modes by a config flip (composition-model §4b:
*the DoorClient is a data-source; the door is the database*).

## Modules & dependency direction (one-way, SACRED)

```
product ──▶ mantlekeep-spring-boot-starter ──▶ mantlekeep-door-client ──▶ mantlekeep-adapter-spi
                                                   ▲
                            (optional, runtime)    │ implements the NativeCore port,
                            mantlekeep-java-core ──────┘ registers via ServiceLoader
```

| module | what it is | deps |
|---|---|---|
| `mantlekeep-adapter-spi` | the ports (`PolicyEvaluator`, `StorePort`, `WorkerPort`, `AgentPort`) + the `AdapterProvider` ServiceLoader contract | none (pure JDK) |
| `mantlekeep-door-client` | the framework-agnostic door: `DoorClient` interface, `ServiceDoorClient` (HTTP, JDK `java.net.http`), `EmbeddedDoorClient` (in-process core behind the `NativeCore` port), `DoorClientFactory` | spi only |
| `mantlekeep-spring-boot-starter` | the ergonomics: auto-configuration builds the `DoorClient` bean from properties; `@MantleKeepIntent` + Spring AOP governs BEFORE execution | door-client + Spring Boot |
| `mantlekeep-java-core` | the thin Panama/FFM binding over the Rust core's C ABI (the SACRED five-symbol surface, adapted from `spike/rust-core/java-core/`) — an **optional runtime drop-in**, Java 22+ | door-client |
| `mantlekeep-adapter-store-inmemory` | **example adapter SDK**: a `StorePort` adapter, config name `inmemory` | spi only |
| `mantlekeep-adapter-policy-allowlist` | **example adapter SDK**: a `PolicyEvaluator` adapter, config name `allowlist` | spi only |
| root `src/` | the original flat, dependency-free sidecar client (`dev.mantlekeep.MantleKeepClient`) — kept as-is for the sidecar demo path | none |

Adding a backend never edits these modules: implement a port, register the provider,
select it by name in config. Library modules are additive-only, semver, ABI-minded.

## Adding an adapter SDK (the extension recipe)

The two `mantlekeep-adapter-*` modules are the worked example — two independent jars, two
different kinds, one SPI, the core untouched. To add your own:

1. **New Gradle module** depending on `mantlekeep-adapter-spi` ONLY (one-way: adapter → spi,
   never the core, never another adapter).
2. **Implement the port** you are backing (`StorePort`, `PolicyEvaluator`, `WorkerPort`,
   `AgentPort`) — one concern per file.
3. **Implement `AdapterProvider`**: `name()` is the string config will select, `kind()`
   pins the port, `create(config)` builds the adapter from its own config subtree.
4. **Register it** in `src/main/resources/META-INF/services/dev.mantlekeep.spi.AdapterProvider`
   (one fully-qualified provider class per line).
5. **Select it by name in config** — e.g. `mantlekeep.door.adapters.store: inmemory` or
   `adapters.policy-evaluator: allowlist`. Putting the jar on the classpath is what makes
   the name legal; an unknown name fails fast at startup listing the registered menu.

Each example module's test proves the loop with the real machinery
(`AdapterCatalog.discover()` → select by name → drive the port → reject
`com.evil.Backdoor`); copy that test shape into your module.

## The no-code flow

```groovy
dependencies { implementation 'dev.mantlekeep:mantlekeep-spring-boot-starter:0.1.0' }
```

```yaml
# application.yml — the ENTIRE integration
mantlekeep:
  door:
    mode: service                      # the one config flip: service | embedded
    url: http://mantlekeep-door:8080       # service mode: the shared remote door
    brand: mantlekeep                      # default "mantlekeep" — the <brand>.rbac namespace (white-label seam)
    subject: lead-bob                  # fallback identity; a real product declares ONE SubjectResolver bean (SSO)
    # dev-login-user: lead-bob         # dev tier only; in prod the SSO gateway owns the session

    # embedded mode instead (tests / dev / the sovereign air-gap zone — §4f):
    # mode: embedded
    # core-path: /opt/mantlekeep/libmantlekeep_core.dylib
    # policy-paths:
    #   - /opt/mantlekeep/policies/rbac.json
    # adapters:
    #   native-core: panama            # names select from the ServiceLoader-REGISTERED set
    #   # store: postgres              # (roadmap) chain store adapter, same rule
```

```java
@Service
public class ReleaseService {

    @MantleKeepIntent(action = "job.promote", resource = "project/demo", goal = "ship 1.2")
    public void promoteToProduction() {
        // reaching this line MEANS the door allowed it and the decision is on the chain;
        // a deny threw DoorDeniedException (with the door's reason) and this body never ran.
    }
}
```

That is the whole product-side surface: the starter's auto-configuration
(`MantleKeepAutoConfiguration`) builds the `DoorClient` from `MantleKeepProperties` via
`DoorClientFactory`, and `MantleKeepIntentAspect` submits the intent through the door
**before** every annotated method — the Spring-Security-filter-chain shape, judged by
MantleKeep. No `mantlekeep.door.mode` property → the starter is completely inert.

Programmatic use (no Spring) is the same client:
`DoorClientFactory.create(DoorConfig.service(url)).submit(Intent.of("job.promote", "ship 1.2"))`.

## SECURITY — ServiceLoader, never `Class.forName(config)`

Config-driven wiring is the goal; **hand-rolled reflection is not the mechanism.** In a
governance product, `Class.forName(someConfigString)` is a config-injection hole: whoever
writes config executes arbitrary code inside the process that grants approvals. This SDK
therefore rides two safe mechanisms only: **Spring DI** (the runtime's own, auditable
wiring) and **Java's ServiceLoader SPI** (`META-INF/services`) for adapter discovery.
Every adapter — including the native-core binding — is SELECTED BY NAME from the set
providers have **registered**; an unknown name fails fast at startup listing what is
registered. Config chooses policy; it can never reach past the registered set. This is
also air-gap and native-image safe: discovery is static metadata, not reflection over
arbitrary names. (Enforced in `AdapterCatalog` + `DoorClientFactory`; tested in
`DoorClientFactoryTest.unknownBindingNameFailsFastListingTheRegisteredSet`.)

## Build & verify

```sh
./gradlew build     # all six modules + the flat client; runs every test
```

Verified 2026-07-26 (JDK 26, Gradle 9.6.0): **BUILD SUCCESSFUL — 32 tests, 0 failures**
(door-client 17, starter 6 incl. real-AOP govern-before-execute proof against a live
Spring context and a JDK-HttpServer door stub, java-core 2 ServiceLoader/config tests,
example adapter SDKs 7 — real ServiceLoader discovery, select-by-name, backdoor-name
rejection; the discovery tests FAIL when the `META-INF/services` entry is removed, so
they prove the registration, not themselves).
The **native Rust library is NOT built here** — `mantlekeep-java-core` compiles and its
registration tests run without it; loading the real `libmantlekeep_core.dylib` end-to-end
is proven by the spike's parity harness (`spike/rust-core/`), and the library is located
at runtime via `mantlekeep.door.core-path`.
