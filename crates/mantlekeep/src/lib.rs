//! # MantleKeep
//!
//! A **governance framework**: one door every human and AI action passes through, a tamper-evident
//! hash-chained audit trail, and ports & adapters so backends swap by config. The one rule that does
//! not bend — **govern before execute**: the door decides, then the effect runs; a deny aborts before
//! any side effect.
//!
//! This is the umbrella crate — the stable entry point for the Rust surface. The engine crates land
//! here, re-exported, as each stabilises:
//!
//! - [`mantlekeep-core`] — the generic governance engine (door, chain, policy port).
//! - [`mantlekeep-kernel`] — the sovereign sandbox (wasmtime; fuel + memory capped, no network).
//! - [`mantlekeep-audit`] — the tamper-evident hash-chain.
//!
//! [`mantlekeep-core`]: https://crates.io/crates/mantlekeep-core
//! [`mantlekeep-kernel`]: https://crates.io/crates/mantlekeep-kernel
//! [`mantlekeep-audit`]: https://crates.io/crates/mantlekeep-audit
//!
//! ## Status
//!
//! `0.0.x` — early. The name is claimed and the shape is documented; the engine crates re-export
//! through here as they land. Track progress at <https://github.com/mantlekeep/mantlekeep>.

#![forbid(unsafe_code)]

/// The version of this umbrella crate (`CARGO_PKG_VERSION`).
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// The one invariant MantleKeep enforces, in one line — govern before execute.
///
/// Returned as a string so a downstream can assert the framework it linked is the governance one,
/// not a namesake. This is a marker until the engine crates re-export here.
pub const INVARIANT: &str = "govern before execute: the door decides, then the effect runs";

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exposes_its_version_and_invariant() {
        assert!(!VERSION.is_empty());
        assert!(INVARIANT.contains("govern before execute"));
    }
}
