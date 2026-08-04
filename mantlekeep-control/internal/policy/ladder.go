package policy

import (
	mantlekeep "mantlekeep.dev/control"
)

// This file is ONE concern: the deployment's role vocabulary and the seniority checks over it.
//
// The engine understands exactly one thing about roles — their RELATIVE authority (a rank).
// The NAMES are data. Historically the five built-in tier names were a hardcoded package var,
// which pinned every deployment to those exact strings: a bank that named its own tier
// ("L1-Super-Admin") got no rank, so its role could never satisfy a seniority check. The
// RoleLadder makes that vocabulary a REPLACEABLE table — the engine ships a default, and a
// config layer (or a code-side product) can supply its own, so the core holds no hardcoded
// role names beyond the default it ships.

// RoleLadder maps a role NAME to its authority rank (lower = more senior). It is the
// deployment's role vocabulary: the engine ships a default (the five built-in tiers) and a
// config/product may REPLACE it wholesale. Seniority is the ONLY ordering the engine reads;
// the names themselves carry no meaning to the core.
type RoleLadder map[string]int

// DefaultRoleLadder returns the engine's built-in five-tier ladder (lower = more senior). A
// FRESH copy is returned on every call so a caller can never mutate a table other code reads.
func DefaultRoleLadder() RoleLadder {
	return RoleLadder{
		"L0-SuperAdmin": 0, "L1-Architect": 1, "L2-Operator": 2, "L3-Consumer": 3, "AI-Agent": 4,
	}
}

// LadderFrom derives the deployment's role vocabulary from a layer set: the FIRST layer that
// declares a non-empty Roles map defines it (REPLACE semantics — that map IS the ladder). The
// ladder is a single vocabulary declared once in the platform/base layer; lower (team/scope)
// layers reference role names, they never redefine the vocabulary — so ladders are NOT merged
// across layers, first non-empty wins. No layer declaring Roles → the built-in default.
func LadderFrom(layers ...Layer) RoleLadder {
	for _, l := range layers {
		if len(l.Roles) > 0 {
			out := make(RoleLadder, len(l.Roles))
			for name, rank := range l.Roles {
				out[name] = rank
			}
			return out
		}
	}
	return DefaultRoleLadder()
}

// holdsAtLeast reports whether any of the subject's roles is at least as senior as need, per
// THIS ladder. An unknown need — or a subject whose roles are all unknown to the ladder — is
// never senior enough: an unknown role can never satisfy a seniority floor.
func (l RoleLadder) holdsAtLeast(roles []string, need mantlekeep.Role) bool {
	nr, ok := l[string(need)]
	if !ok {
		return false
	}
	for _, r := range roles {
		if rk, ok := l[r]; ok && rk <= nr {
			return true
		}
	}
	return false
}

// atLeastAsSenior reports whether role a is at least as senior as b (a can stand in for b),
// per THIS ladder. An unknown role on either side is never senior enough — so an unknown
// override can never loosen a sealed floor.
func (l RoleLadder) atLeastAsSenior(a, b mantlekeep.Role) bool {
	ra, oka := l[string(a)]
	rb, okb := l[string(b)]
	if !oka || !okb {
		return false
	}
	return ra <= rb
}
