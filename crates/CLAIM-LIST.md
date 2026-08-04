# crates.io name claims — MantleKeep

crates.io is a **flat, name-based** namespace — no prefix ownership, no reverse-DNS. A name is held
only by **publishing a crate** with it. You cannot reserve `mantlekeep-*` as a wildcard; each name is
claimed individually. crates.io **discourages pure squatting** (empty placeholders can be reclaimed),
so each crate below is minimal-but-real: a description, a doc comment stating intent, and it compiles.

## Priority — publish NOW (highest squat risk)

| Crate | Purpose | State |
|---|---|---|
| **`mantlekeep`** | Umbrella / entry point; engine crates re-export here as they stabilise. | ✅ ready in `crates/mantlekeep/` — publish first |

## Engine crates — publish as each takes shape (real crates, not empty placeholders)

| Crate | Purpose (from the architecture) |
|---|---|
| `mantlekeep-core` | The generic governance engine — door, chain, policy port. Knows zero product/action names. |
| `mantlekeep-kernel` | The sovereign sandbox — wasmtime, fuel + memory capped, no network. |
| `mantlekeep-audit` | The tamper-evident hash-chain (append-only, verifiable). |
| `mantlekeep-door` | The door primitive — submit an intent, get a decision, record it. |
| `mantlekeep-wasm` | The WASM tool runtime (a governed step = a wasm module). |
| `mantlekeep-ffi` | The FFI / UniFFI bindings surface (the Rust core embedded via Panama/PyO3/etc.). |

## How to publish each (you run these — publishing is irreversible + outward-facing)

```bash
cargo login                     # once — paste your crates.io API token (crates.io → Account Settings)
cd crates/mantlekeep
cargo publish --dry-run         # verify it packages cleanly (no publish)
cargo publish                   # claims the name — PERMANENT, cannot be un-published (only yanked)
```

**Notes**
- Publish **`mantlekeep` first** — it is the name a squatter grabs.
- For the engine names, publish a **minimal real crate each** as you build it — a stub `lib.rs` with a
  doc comment + a marker const compiles and passes the no-squat sniff. Batch-publishing empty crates
  risks reclamation; publishing early-but-genuine crates does not.
- Versions are **immutable** (like Maven Central) — you can only `cargo yank`, never delete. Start at
  `0.0.1` so the real releases have room.
- Owns-the-name ≠ trademark. Register the **word mark** separately for real name protection.
