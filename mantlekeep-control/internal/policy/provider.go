package policy

import mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"

// PolicyProvider is the PORT a product's policy adapter implements. It is how the core stays
// GENERIC: the engine holds no product action names, role grants, or attribute floors of its
// own — a product supplies them and the core loads them. This is the ports-&-adapters pattern
// applied to policy: a product's action vocabulary lives in adapters that implement this
// interface (or in the shared grants document), never named in the core engine.
//
// A provider declares three things:
//   - Actions()      the action names it owns (so the core can route admission to it)
//   - RoleActions()  which roles may issue those actions (folded into the generic role check)
//   - Admit()        the attribute floor over intent.Params (an ABAC check the core delegates)
//
// The core registers providers at wiring time (WithProviders) — a compile-time adapter, per the
// owner's stated design. It never imports a provider package; it holds only this interface, so
// the generic core compiles with no product dependency. The deployable's main wires the core +
// the adapters it ships.
type PolicyProvider interface {
	// Name identifies the product (audit/debug only).
	Name() string
	// Actions returns the action names this provider owns. The core builds an action→provider
	// index from these and routes each owned action's admission check to this provider.
	Actions() []string
	// RoleActions maps a role to the actions this provider grants it. The core folds these into
	// its generic role check, so a provider's actions are authorized by the SAME rule as the
	// core's own — no second engine.
	RoleActions() map[mantlekeep.Role][]string
	// Admit applies the product's ATTRIBUTE floor to ONE intent it owns. It returns (true,
	// reason) to DENY, (false, "") to allow. roles are the caller's roles (some floors, e.g. an
	// irreversible-op gate, read them). The core calls it only for actions this provider owns,
	// and only AFTER the generic role check has already permitted the action.
	Admit(intent mantlekeep.PolicyIntent, roles []mantlekeep.Role) (deny bool, reason string)
}

// HoldsAtLeast reports whether any role is at least as senior as need, per the core's generic
// authority ranking. It is EXPORTED so a product provider's attribute floor can gate an action
// on seniority (e.g. an irreversible op) without duplicating the rank table — the ranking is a
// generic governance concept and lives once, here in the core.
//
// This helper has no RBAC receiver, so it ranks against the DEFAULT ladder: a provider gating on
// seniority reasons in the default vocabulary. The per-deployment configured ladder (set via
// RBAC.WithRoleLadder) governs the actual door decisions inside RBAC — a deployment that renames
// its tiers wires that ladder there, not here.
func HoldsAtLeast(roles []mantlekeep.Role, need mantlekeep.Role) bool {
	return DefaultRoleLadder().holdsAtLeast(rolesToStrings(roles), need)
}
