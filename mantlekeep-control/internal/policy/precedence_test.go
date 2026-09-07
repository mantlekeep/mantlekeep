package policy

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// layerOnly is a minimal ActionAuthorizer standing in for a resolved scope layer.
type layerOnly map[string]mantlekeep.Role

func (l layerOnly) RequiredRole(action string) (mantlekeep.Role, bool) {
	role, ok := l[action]
	return role, ok
}

// withDocument seeds the merged grant document for one test and restores it afterwards. The
// cache is package state loaded once; seeding it directly rather than through a file keeps
// these tests about PRECEDENCE and not about loading.
func withDocument(t *testing.T, document map[string]map[string]bool) {
	t.Helper()
	ensurePolicy()
	restore := roleActionsCache
	roleActionsCache = document
	t.Cleanup(func() { roleActionsCache = restore })
}

// PINS THE PRECEDENCE RULE: a layer that names an action DECIDES it.
//
// A grant document (role_actions) says what a role is FOR; the layer cascade (default →
// platform → team → scope) says what this organisation permits WHERE. When both speak, the
// deployment wins: grant is NECESSARY, the cascade is SUFFICIENT-TO-REFUSE.
//
// The document used to win outright — actionAllowed returned true the moment a document named
// the action for one of the subject's roles, so a scope file saying
//
//	scopes/example.json  {"actionRoles": {"service.deploy": "L1-Architect"}}
//
// asserted NOTHING if the document granted service.deploy to a consumer. The file was read,
// the layer loaded, the boot log said so — and every consumer still deployed to that scope.
//
// If this test fails, someone has put the document back in front of the cascade. Do not "fix"
// it by relaxing the assertion: a scope layer that cannot tighten is a governance control that
// governs nothing, which is the failure this whole engine exists to prevent. Change it
// deliberately, as a release, alongside the rule in precedence.go.
func TestATighterScopeLayerOverridesAGrantDocument(t *testing.T) {
	engine := NewRBAC()

	// The scope layer demands an architect for this action.
	scope := layerOnly{"service.deploy": mantlekeep.RoleArchitect}

	// A consumer, whom the scope layer alone would refuse.
	consumer := []string{string(mantlekeep.RoleConsumer)}

	if engine.actionAllowed(consumer, "service.deploy", scope) {
		t.Fatal("PRECONDITION: with no grant document, the scope layer must refuse a " +
			"consumer — if this fires, the cascade itself is broken, not just its precedence")
	}

	// Now let a grant document name the action for that role, as grants.json does.
	withDocument(t, map[string]map[string]bool{
		string(mantlekeep.RoleConsumer): {"service.deploy": true},
	})

	if engine.actionAllowed(consumer, "service.deploy", scope) {
		t.Fatal("REGRESSION: a grant document has overridden a tighter scope layer again. " +
			"The layer named service.deploy and required L1-Architect; a document granting it " +
			"to L3-Consumer must NOT reopen it. A scope file that cannot tighten is a control " +
			"that governs nothing")
	}

	// The same action, same document, same layer — and an architect still passes. Tightening
	// must refuse the people the layer names, not everybody: a rule that denied here would
	// look identical in a smoke test and would have taken the action away from its owner.
	if !engine.actionAllowed([]string{string(mantlekeep.RoleArchitect)}, "service.deploy", scope) {
		t.Fatal("the scope layer requires L1-Architect and an architect was refused — " +
			"tightening has become a blanket deny, which is a different bug wearing the same face")
	}
}

// An action no layer names is decided by the document alone — the long-standing behaviour,
// pinned separately so a future change to the precedence rule cannot quietly take the
// documents' authority away from every action a deployment never wrote a layer for.
func TestAnActionNoLayerNamesIsStillDecidedByTheDocument(t *testing.T) {
	engine := NewRBAC()
	withDocument(t, map[string]map[string]bool{
		string(mantlekeep.RoleConsumer): {"service.plan": true},
	})

	// A layer exists and is consulted — it simply has nothing to say about service.plan.
	scope := layerOnly{"service.deploy": mantlekeep.RoleArchitect}
	consumer := []string{string(mantlekeep.RoleConsumer)}

	if !engine.actionAllowed(consumer, "service.plan", scope) {
		t.Fatal("a document-granted action that no layer names must still be allowed: the " +
			"cascade tightens what it names, it does not revoke what it is silent about")
	}
	if engine.actionAllowed(consumer, "service.destroy", scope) {
		t.Fatal("neither the document nor a layer grants service.destroy — that must be a deny")
	}
}

// A layer naming a role the ladder cannot RANK refuses everyone, the wildcard holder included.
//
// Pinned because it is the one way this rule can lock a deployment out of its own action, and
// because the permissive alternative — treating an unrankable role as "no opinion" — would
// make a typo silently reopen the action the layer was written to close. resolve.go already
// reads this way (atLeastAsSenior refuses an unknown override), ValidateLayers refuses startup
// on it, and the boot diagnostic (notices.go) names the file that wrote it.
func TestALayerRoleTheLadderCannotRankRefusesEveryone(t *testing.T) {
	engine := NewRBAC()
	withDocument(t, map[string]map[string]bool{string(mantlekeep.RoleSuperAdmin): {"*": true}})

	typo := layerOnly{"service.deploy": mantlekeep.Role("L1-Architech")}

	if engine.actionAllowed([]string{string(mantlekeep.RoleSuperAdmin)}, "service.deploy", typo) {
		t.Fatal("a layer naming an unrankable role must refuse — including the wildcard " +
			"holder. Allowing here would mean a misspelled role name silently reopens the " +
			"action the layer exists to close, and nothing would report it")
	}
}

// The wildcard holder must not be UNDERSTATED. A layer that names an action names a MINIMUM,
// and a role senior to that minimum satisfies it — the wildcard's authority comes from the
// ladder, not from the grant document, so a layer the wildcard holder outranks must still let
// them through.
//
// Pinned because the tightening rule reaches every action a layer names, and the cheapest way
// to get it wrong is to read "the document no longer decides" as "the document's wildcard no
// longer counts". That would lock the most senior tier out of the actions a deployment
// tightened, which is precisely the tier called on to fix a tightening that went wrong.
func TestTheWildcardHolderIsNotUnderstatedByALayerItOutranks(t *testing.T) {
	engine := NewRBAC()
	withDocument(t, map[string]map[string]bool{string(mantlekeep.RoleSuperAdmin): {"*": true}})

	layer := layerOnly{"service.deploy": mantlekeep.RoleArchitect}

	if !engine.actionAllowed([]string{string(mantlekeep.RoleSuperAdmin)}, "service.deploy", layer) {
		t.Fatal("the wildcard holder outranks the role this layer requires and was refused — " +
			"a tightening has locked out the tier that has to undo it")
	}
	// And the same layer still refuses the tier below the one it names, or the assertion above
	// would be satisfied by a rule that allows everybody.
	if engine.actionAllowed([]string{string(mantlekeep.RoleOperator)}, "service.deploy", layer) {
		t.Fatal("the layer requires L1-Architect and an L2-Operator passed it")
	}
}

// THE CLAIM THIS CHANGE MUST NOT BREAK: nothing became more permissive.
//
// The rule moved from "document, then layer as a fallback" to "layer if it names the action,
// document otherwise". Written as logic, with D = a document grants it to one of these roles,
// L = a layer names a required role, H = the subject holds at least that role:
//
//	before:  D ∨ (L ∧ H)
//	after:   (L ∧ H) ∨ (¬L ∧ D)
//
// Every case the new rule allows the old rule allowed too. Rather than trust that paragraph,
// this walks the whole cross-product of documents, layers and subjects and asserts the
// implication directly against a literal transcription of the OLD code.
//
// If this fails, STOP. A case that became more permissive is the one unacceptable outcome of
// this change: it means some deployment silently gained an ability nobody granted it.
func TestNothingBecameMorePermissive(t *testing.T) {
	documents := map[string]map[string]map[string]bool{
		"empty":                   {},
		"consumer granted":        {"L3-Consumer": {"service.deploy": true}},
		"operator granted":        {"L2-Operator": {"service.deploy": true}},
		"wildcard superadmin":     {"L0-SuperAdmin": {"*": true}},
		"grants another action":   {"L3-Consumer": {"service.plan": true}},
		"consumer and superadmin": {"L3-Consumer": {"service.deploy": true}, "L0-SuperAdmin": {"*": true}},
	}
	layers := map[string]ActionAuthorizer{
		"no layer at all":        nil,
		"layer silent":           layerOnly{"unrelated.action": mantlekeep.RoleOperator},
		"layer wants architect":  layerOnly{"service.deploy": mantlekeep.RoleArchitect},
		"layer wants consumer":   layerOnly{"service.deploy": mantlekeep.RoleConsumer},
		"layer wants superadmin": layerOnly{"service.deploy": mantlekeep.RoleSuperAdmin},
		"layer wants a typo":     layerOnly{"service.deploy": mantlekeep.Role("L1-Architech")},
	}
	subjects := map[string][]string{
		"nobody":     {},
		"consumer":   {"L3-Consumer"},
		"operator":   {"L2-Operator"},
		"architect":  {"L1-Architect"},
		"superadmin": {"L0-SuperAdmin"},
		"ai agent":   {"AI-Agent"},
	}
	actions := []string{"service.deploy", "service.plan", "never.granted"}

	engine := NewRBAC()
	// Seeded directly rather than through withDocument: this loop swaps the document many
	// times, and a stack of deferred restores is a slower way of saying "put it back once".
	ensurePolicy()
	original := roleActionsCache
	defer func() { roleActionsCache = original }()

	compared := 0
	for documentName, document := range documents {
		roleActionsCache = document
		for layerName, layer := range layers {
			for subjectName, roles := range subjects {
				for _, action := range actions {
					now := engine.actionAllowed(roles, action, layer)
					before := allowedUnderTheOldRule(engine, roles, action, layer)
					compared++
					if now && !before {
						t.Fatalf("MORE PERMISSIVE: %q + %q + %q on %q is allowed now and was "+
							"denied before. This change may only ever refuse more; a case that "+
							"gains an ability is a governance regression, not a fix — stop and "+
							"report it rather than adjusting this test",
							documentName, layerName, subjectName, action)
					}
				}
			}
		}
	}
	// A guard that compared nothing passes for the wrong reason, and this one is the whole
	// evidence for "nothing became more permissive".
	if want := len(documents) * len(layers) * len(subjects) * len(actions); compared != want {
		t.Fatalf("compared %d combinations, want %d — the oracle did not cover the table it "+
			"claims to", compared, want)
	}
}

// allowedUnderTheOldRule is a LITERAL transcription of actionAllowed as it stood before the
// precedence change: the document answered first and returned on the spot, and the layer
// cascade was consulted only as a fallback. It exists so the "nothing became more permissive"
// claim is checked against the real old code path rather than against a description of it.
func allowedUnderTheOldRule(r *RBAC, roles []string, action string, dyn ActionAuthorizer) bool {
	for _, ro := range roles {
		acts := roleActions()[ro]
		if acts["*"] || acts[action] {
			return true
		}
		if r.providerRoleActions[ro][action] {
			return true
		}
	}
	if dyn != nil {
		if need, ok := dyn.RequiredRole(action); ok {
			return r.rankLadder().holdsAtLeast(roles, need)
		}
	}
	return false
}
