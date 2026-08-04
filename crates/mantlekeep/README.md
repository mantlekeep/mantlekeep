# mantlekeep

**MantleKeep — a governance framework.** One door every human and AI action passes through, a
tamper-evident hash-chained audit trail, and ports & adapters so backends swap by config. The rule
that does not bend: **govern before execute** — the door decides, then the effect runs; a deny aborts
before any side effect.

This is the **umbrella crate** — the stable entry point for the Rust surface. The engine crates
(`mantlekeep-core`, `mantlekeep-kernel`, `mantlekeep-audit`, …) re-export through here as each
stabilises.

- **Status:** `0.0.x` — early. The name is claimed and the shape is documented; engine crates land here as they land.
- **Repo:** <https://github.com/mantlekeep/mantlekeep>
- **License:** Apache-2.0

```rust
assert!(mantlekeep::INVARIANT.contains("govern before execute"));
```
