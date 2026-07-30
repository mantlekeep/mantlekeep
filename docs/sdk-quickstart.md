# Java SDK quick-start

The Java SDK is the **socket** products plug into: a config-driven door client, the
adapter SPI, example adapters, and Spring Boot starters. Dependency direction is
one-way and strict: `product → starter → door-client → adapter-spi`.

Requires a JDK 25 toolchain. Gradle and Maven are both first-class — same modules, same
tests, same `dev.mantlekeep:*` coordinates. Use whichever your shop standardises on.

```bash
cd sdks/java
./gradlew build          # Gradle
mvn install              # Maven
```

Add the starter and you have governance with no wiring code:

```gradle
implementation "dev.mantlekeep:mantlekeep-spring-boot-starter:0.2.0"
```
```xml
<dependency>
  <groupId>dev.mantlekeep</groupId>
  <artifactId>mantlekeep-spring-boot-starter</artifactId>
  <version>0.2.0</version>
</dependency>
```

## The door client, plain Java

```java
import dev.mantlekeep.MantlekeepClient;
import dev.mantlekeep.Intent;

var mantlekeep = MantlekeepClient.connect("http://localhost:8080").login("lead-bob");

var decision = mantlekeep.govern(
    Intent.action("job.promote").env("PROD").goal("ship the service"));

System.out.println("allowed=" + decision.allowed() + " token=" + decision.token());

// One-line governance guard — throws MantlekeepDeniedException if the door denies:
mantlekeep.govern(Intent.action("job.promote").env("PROD").goal("ship")).require();
```

See `sdks/java/src/main/java/dev/mantlekeep/Example.java` for a runnable version
(needs `mantlekeep serve` on `:8080`).

## With Spring Boot

Add the starter and configure the door under `mantlekeep.door.*`; the
`@MantlekeepIntent` aspect governs an annotated method through the door before it runs.
The starter auto-configures a `DoorClient` bean from config — no wiring code.

```yaml
mantlekeep:
  door:
    mode: service
    url: http://localhost:8080
    brand: mantlekeep          # the <brand>.rbac policy namespace (white-label seam)
```

## Writing an adapter (the extension pattern)

The core knows only a **port**; a backend is an **adapter** that implements the SPI and
registers itself via `META-INF/services`, selected by config name. Two worked examples
ship here:

- `sdks/java/mantlekeep-adapter-store-inmemory/` — a `StorePort` adapter.
- `sdks/java/mantlekeep-adapter-policy-allowlist/` — a `PolicyEvaluator` adapter.

Each registers an `AdapterProvider` in
`META-INF/services/dev.mantlekeep.spi.AdapterProvider`; `AdapterCatalog.discover()`
finds it and `select(kind, name)` picks it by configured name. Copy one of these to
build your own adapter against a real backend — it teaches the pattern without touching
the sealed core.
