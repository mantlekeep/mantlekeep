# Extending MantleKeep — ports, adapters, and config selection

MantleKeep is built to be **extended, not edited**. The core knows only a **port**;
everything a backend knows lives in an **adapter** behind that port. You add a
capability by writing an adapter and *selecting it by config* — you never modify the
core. This is the rule that keeps the governance engine generic and your product
changes isolated.

> **Swap a backend by configuration, not by rewriting the core.**

---

## The ports

A **port** is a small interface the core depends on; an **adapter** implements it for
a specific backend. The Java SPI (`dev.mantlekeep.spi`) defines four:

| Port | Question it answers | Example adapters |
|------|---------------------|------------------|
| `PolicyEvaluator` | May this subject perform this action, given these attributes? | allowlist (shipped), OPA-as-wasm, a rules engine |
| `StorePort` | Where does the append-only audit chain live? | in-memory (shipped), file, Postgres |
| `WorkerPort` | How is one unit of approved work dispatched? | k8s Job emitter, Jenkins trigger, self-pod runner |
| `AgentPort` | Which AI harness runs an agent task (BYOK)? | internal LLM, external API behind the mandatory proxy, none |

Each is deliberately tiny — one or two methods — so it is easy for a human *and* an AI
to implement correctly. For example, the whole policy port is:

```java
public interface PolicyEvaluator {
    // MUST be side-effect free — recording the decision belongs to the chain, not here.
    PolicyVerdict evaluate(String subjectId, String action, Map<String, String> attributes);
}
```

and its answer type carries the reason (a bare "no" is not governable evidence):

```java
PolicyVerdict.allow();
PolicyVerdict.deny("action 'x' is not permitted in PROD");
```

The two **shipped example adapters** are the templates to copy:

- `sdks/java/mantlekeep-adapter-policy-allowlist/` — a `PolicyEvaluator` that allows a
  fixed set of actions and denies everything else (deny-by-default).
- `sdks/java/mantlekeep-adapter-store-inmemory/` — a `StorePort` that keeps the chain
  in process memory.

---

## The adapter SPI: how an adapter is discovered and selected

An adapter enters the SDK through **one** contract, `AdapterProvider`:

```java
public interface AdapterProvider {
    String name();                              // the name config selects it by, e.g. "postgres"
    AdapterKind kind();                         // which port it satisfies
    Object create(Map<String, String> configuration);   // build the instance from its config subtree
}
```

Discovery is by Java's **`ServiceLoader`**: an adapter jar declares its provider in
`META-INF/services/dev.mantlekeep.spi.AdapterProvider`. `AdapterCatalog.discover()`
finds every registered provider on the classpath, and `select(kind, name, config)`
picks one **by name** and verifies it really implements the port for its kind.

### Why `ServiceLoader` and never `Class.forName(configValue)`

This is a security boundary, not a style choice. In a governance product, loading a
class *named by a free-form config string* would be a config-injection hole: whoever
writes config would get arbitrary code execution inside the very process that grants
approvals. `ServiceLoader` inverts that — only classes an artifact on the classpath
has **registered** are discoverable, and config can merely *pick among them by name*.
Config chooses policy; it can never reach past the registered set. An unknown name
fails fast at startup, with the registered names listed — never a surprise mid-request.
It is also air-gap and native-image safe: discovery is static metadata, no reflection
over arbitrary names.

---

## Add your own adapter — the four steps

Copy one of the shipped example modules and change four things. Using a `StorePort`
backed by Postgres as the example:

**1. Implement the port.**

```java
package com.acme.mantlekeep.store.postgres;

import dev.mantlekeep.spi.StorePort;

public final class PostgresStore implements StorePort {
    // ... open a connection pool from config, append/read the chain ...
    @Override public void append(String auditRecordJson) { /* INSERT append-only */ }
    @Override public java.util.List<String> readAll()   { /* SELECT ordered by seq */ }
}
```

**2. Register a provider** that names it and states its kind:

```java
public final class PostgresStoreProvider implements AdapterProvider {
    @Override public String name()      { return "postgres"; }
    @Override public AdapterKind kind() { return AdapterKind.STORE; }
    @Override public StorePort create(Map<String, String> configuration) {
        return new PostgresStore(configuration.get("url"), configuration.get("user"));
    }
}
```

**3. Declare it for `ServiceLoader`** — add a line to
`src/main/resources/META-INF/services/dev.mantlekeep.spi.AdapterProvider`:

```
com.acme.mantlekeep.store.postgres.PostgresStoreProvider
```

**4. Put the jar on the runtime classpath.** That — and only that — makes
`store: postgres` a legal config value. Nothing else registers it; nothing outside the
registered set is reachable.

Your adapter is a **separate jar**. Its driver dependencies (the Postgres JDBC driver,
etc.) live in *that* jar, never in the core — so a CVE in a backend driver is isolated
to the adapter that uses it, not dragged onto the governance engine's classpath.

---

## How config selects an adapter

Selection is name-into-the-registered-set, by `AdapterKind`'s config key:

| Kind | Config key |
|------|------------|
| `PolicyEvaluator` | `policy-evaluator` |
| `StorePort` | `store` |
| `WorkerPort` | `worker` |
| `AgentPort` | `agent` |

**Environment variables** use the `MANTLEKEEP_*` namespace; **Spring properties** use
`mantlekeep.*`. For the door client, adapter selections live under
`mantlekeep.door.adapters.*` and each adapter's own settings under its subtree:

```yaml
mantlekeep:
  door:
    adapters:
      store: postgres            # ← selects the registered provider named "postgres"
      policy-evaluator: allowlist
    # each adapter's own config subtree (flat key → value), handed to create():
    store-config:
      url: jdbc:postgresql://db/audit
    policy-evaluator-config:
      allowed-actions: session.start,session.stop
```

If config names an adapter that no registered provider matches, startup fails
immediately with the menu of what *is* registered — a typo can never become a
mid-request failure.

The **allowlist** example reads a `allowed-actions` key (comma-separated action
names); a missing or blank value means an **empty** allowlist — deny everything. A
policy adapter never defaults open.

---

## The embedded-core binding (a special port)

One selection point is separate from the four adapter kinds: which native binding
**embeds** the governance core in-process. It has its own `ServiceLoader` contract,
`NativeCoreProvider`, selected by `mantlekeep.door.adapters.native-core`. The
`DoorClient` runs in one of two modes, chosen by config alone:

- `mode: service` — call a shared remote door over HTTP (the production shape for
  scaled pods behind one door).
- `mode: embedded` — carry a real in-process core (the sovereign air-gap shape: each
  zone runs its own door + chain).

Products depend only on the `DoorClient` interface; `DoorClientFactory` picks the
implementation from config. Same security rule: the binding is selected from the
`ServiceLoader`-registered set, never by class name.

---

## The rules that don't bend

- **Extend, don't edit.** Add adapters; never modify the core to make room for a
  backend. Dependency direction is one-way: `product → starter → door-client →
  adapter-spi`. The SPI never depends on your adapter.
- **Deny-by-default.** With no policy loaded, the door denies. A policy adapter that
  can't decide must deny, not allow.
- **A verdict is side-effect free; recording belongs to the chain.** The
  `PolicyEvaluator` answers; the `StorePort` records. Don't blur them.
- **Config selects; it never injects.** Names pick from the registered set. That is
  the sealed-floor rule applied to wiring — config can choose policy, but it can never
  reach the guarantee.

See [build-your-first-product.md](build-your-first-product.md) for a runnable worked
example, and [architecture.md](architecture.md) for how the ports sit in the whole.
