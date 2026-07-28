# WHAT: the assembly seam (`app`)

**Plain-language walkthrough for a non-Go reviewer.** No Go knowledge required.

## Purpose (one paragraph)

This package is the **wiring harness** — the place where all the separate parts
(identity, policy, audit, the run engine, the web Portal) are snapped together into one
running server. It exists so the *lean core* never has to link heavy optional pieces:
the core defines the sockets (ports), `app` plugs implementations into them, and a
separate adapter module (e.g. OPA for policy, NATS for distributed runs) can supply a
different plug **without the core ever importing that heavy dependency**. It is also
where MantleKeep's **config cascade** is stitched together at boot: it reads the layer files
from the environment (default → platform/host → team), resolves them into one effective
policy honouring the sealed floors, and hands that to the policy engine. If you want to
know "how do the layers actually get loaded and wired in?", this is the file to read.

## The key decisions, with file:line anchors

| Decision | Where | What it means |
|---|---|---|
| Options = the swap points | `app.go:29-43` | Two things are overridable: `Policy` (swap the whole policy engine) and `Runner` (swap the step executor). Leaving them empty (`nil`) selects the lean built-in defaults. This is *the* extension seam. |
| `Serve` = the boot sequence | `app.go:49-104` | Reads top-to-bottom as the assembly order: build identity → load products → build the door → pick the runner → open durable stores → build the Portal → listen. |
| One product registry, shared | `app.go:56-58` | The *same* product list feeds both the door's authorization (a product's `RunAs` role) and the Portal's run engine (its steps). One source of truth for "products are config". |
| Policy swap without linking | `app.go:33`, `door.go:23-28` | If `opts.Policy` is supplied it replaces the default RBAC; otherwise the pure-Go RBAC is used. The core module's dependency graph never sees OPA. |
| Runner swap without linking | `app.go:39`, `app.go:63-66` | Same trick for the run transport: default is the in-process LocalRunner; NATS can inject a distributed one. |
| Durable stores, not wiped | `app.go:70-80`, `door.go:31-34` | Run history (`events.db`), loop state (`loops.db`) and the audit chain (`audit.db`) are opened as files in the data dir, so everything survives a restart. |

### Where the config layers wire in — `door.go` + `policyconfig.go`

This is the answer to *"how does default → platform → product → team actually get built?"*

| Step | Where | What it means |
|---|---|---|
| Start with MantleKeep's baseline | `door.go:55` | `policy.DefaultLayer()` is always layer 0 (the built-in promote gate, unsealed). |
| Legacy flat gate (compat) | `door.go:57-60` | An older `MANTLEKEEP_PROMOTE_GATE=...` env string is honoured as a low-precedence layer. |
| **Platform (host) layer — the floor** | `door.go:62-64` | Loaded from `MANTLEKEEP_PLATFORM_CONFIG`. Added **before** the team layer, so its `sealed` keys bind everything after it. |
| **Team layer — most specific** | `door.go:65-67` | Loaded from `MANTLEKEEP_TEAM_CONFIG`. Wins on unsealed keys, but cannot loosen a sealed floor. |
| Resolve the cascade | `door.go:69` | `policy.Resolve(layers...)` collapses the ordered layers into one effective config with seals enforced. |
| Product layer as fallback | `door.go:70-72` | The product registry is attached as the last resort for actions the layers don't name. |
| Feed it to the engine | `door.go:73` | `NewRBAC().WithResolved(resolved)` — the resolved cascade becomes the live law. |
| What a layer file looks like | `policyconfig.go` (`layerFile`) | Plain JSON: `actionRoles`, `sealed`. No new dependency, same env-config style as the rest of MantleKeep. (Env-gating of an action is a product's floor DATA, not a layer key.) |
| A missing/broken layer is ignored, not fatal | `policyconfig.go:36-45` | Unset env → skip; bad JSON → warn on stderr and skip. Boot never dies on a bad optional layer. |

## How to review this WITHOUT reading Go

1. **Read `Serve` (`app.go:49-104`) as a numbered checklist.** Each block is one wiring
   step and has a comment saying why. You are checking the *order* and that nothing
   heavy is imported unconditionally — not the mechanics.
2. **Find the two swap points.** `Options` at `app.go:29-43` has exactly two overridable
   fields, `Policy` and `Runner`. The whole "core stays lean, adapters inject heavy deps"
   story reduces to: *if the field is empty, use the built-in; if supplied, use theirs.*
   Confirm that's what `door.go:24-28` and `app.go:63-66` do.
3. **Follow the four layers by env var name.** In `door.go:54-73` the layers are appended
   in this exact order: default → (legacy flat) → platform → team, then product as
   fallback. The comment block at `door.go:47-53` states the same order in English. The
   ordering is the whole point: platform is added before team so its seals win.
4. **Open the example JSON, not the Go.** `examples/policy/platform.json` (the host floor)
   and `examples/policy/team.json` (the team override) are what `policyconfig.go` reads.
   Reading those two files tells you the actual rules in force without any Go at all.
5. **Prove it by running.** Build and run per the task brief, then start once with
   `MANTLEKEEP_PLATFORM_CONFIG=examples/policy/platform.json MANTLEKEEP_TEAM_CONFIG=examples/policy/team.json`.
   On boot the server prints one `policy: layer "..." loaded (N promote, N action, N sealed)`
   line per layer (`policyconfig.go:52-53`) — visible proof of which layers wired in and
   in what order.
