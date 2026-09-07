package policy

// This file holds ONE decision: when a grant document and the layer cascade both have
// something to say about an action, which of them wins.
//
// # The rule
//
//	GRANT is NECESSARY.  The cascade is SUFFICIENT-TO-REFUSE.
//
// A document (role_actions, or a registered product provider's role grants) says what a role
// is FOR. The cascade (MantleKeep default → platform → team → scope) says what THIS
// organisation permits WHERE. When both name an action, the deployment wins — because a
// platform layer whose sealed keys are a floor is not a floor if a product's own document can
// walk past it, and a tenant that cannot tighten its own scope is not a tenant.
//
// So an action must satisfy BOTH:
//
//	document grants it, no layer names a role for it        → allow (the document decides alone)
//	document grants it, a layer names a role held           → allow
//	document grants it, a layer names a role NOT held       → DENY  ← this is what changed
//	document does not grant it, a layer names a role held   → allow (the long-standing fallback)
//	neither                                                 → deny
//
// Mechanically that is one sentence: when a layer names a required role, that role decides;
// otherwise the document decides.
//
// # Why the order is the whole implementation
//
// Previously the document was asked FIRST and returned true on the spot, so the cascade was a
// fallback rather than a floor — it decided only actions no document granted. A scope file
// saying {"actionRoles": {"service.deploy": "L1-Architect"}} therefore asserted NOTHING when
// the document already granted service.deploy to a consumer: the file was read, the layer
// loaded, the boot log named it, and every consumer still deployed. The operator got positive
// feedback for a control that governed nothing — the exact failure this engine exists to
// prevent.
//
// # This can only ever REFUSE more
//
// Reading the table above as logic, with D = "a document grants it to one of these roles",
// L = "a layer names a required role", H = "the subject holds at least that role":
//
//	before:  D ∨ (L ∧ H)
//	after:   (L ∧ H) ∨ (¬L ∧ D)
//
// Every case the new rule allows, the old rule allowed too — (L∧H) is a term of both, and
// (¬L∧D) implies D. Nothing anywhere becomes more permissive; the only cases that move are
// D ∧ L ∧ ¬H, which move from allow to deny. TestNothingBecameMorePermissive walks the whole
// truth table rather than trusting that paragraph.
//
// # The one surprise, named on purpose
//
// A layer naming a role the deployment's ladder does not RANK (a typo: "L1-Architech") refuses
// everyone, including the L0-SuperAdmin wildcard — RoleLadder.holdsAtLeast cannot honour a role
// it cannot order, and guessing in the permissive direction is how a floor becomes a default.
// That matches RoleLadder.atLeastAsSenior, where an unknown override can never loosen a sealed
// key, and ValidateLayers, which refuses startup on such a layer. The boot diagnostic
// (notices.go) names such a role together with the file that wrote it.

// actionAllowed decides the role check for one action: the layer cascade if it names the
// action, the grant documents otherwise. See the file comment for why in that order.
func (r *RBAC) actionAllowed(roles []string, action string, dyn ActionAuthorizer) bool {
	// The cascade is asked FIRST because it is the only one of the two that can REFUSE. A
	// resolved layer that names a required role for this action has already been through
	// Resolve's seal rule, so what arrives here is the deployment's settled answer — team over
	// platform where free, never looser than a sealed floor.
	if dyn != nil {
		if need, ok := dyn.RequiredRole(action); ok {
			return r.rankLadder().holdsAtLeast(roles, need)
		}
	}
	// No layer named this action, so the documents decide alone — unchanged behaviour for
	// every action a deployment has not written a layer for, which is most of them.
	return r.grantedByDocument(roles, action)
}

// grantedByDocument reports whether any of the subject's roles is granted the action by a
// GRANT DOCUMENT: the merged role_actions document (baseline ∪ platform ∪ products, including
// the engine's own L0-SuperAdmin wildcard) or a registered product provider's role grants.
//
// Providers are treated as documents, not as layers, because they are the same KIND of
// statement: a product declaring what its own roles are for. A deployment that wants one of
// them tightened writes a layer, and the rule above lets that layer bind.
func (r *RBAC) grantedByDocument(roles []string, action string) bool {
	for _, role := range roles {
		granted := roleActions()[role]
		if granted["*"] || granted[action] {
			return true
		}
		if r.providerRoleActions[role][action] {
			return true
		}
	}
	return false
}
