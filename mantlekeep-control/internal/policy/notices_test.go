package policy

import (
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// The diagnostic exists so the FIRST deployment to meet the precedence rule reads about it
// instead of discovering it as a refusal nobody can explain. These tests hold it to the two
// things that make it usable at 3am: it names the FILE, and it names the ACTION.
//
// The grant document is seeded here rather than loaded, because the core ships an EMPTY one —
// it names no product action. A test that read the shipped document would assert nothing.
func withDeployGrantedToThreeTiers(t *testing.T) {
	t.Helper()
	withDocument(t, map[string]map[string]bool{
		"L0-SuperAdmin": {"*": true},
		"L1-Architect":  {"service.deploy": true},
		"L2-Operator":   {"service.deploy": true},
		"L3-Consumer":   {"service.deploy": true},
	})
}

func TestTheDiagnosticNamesTheFileAndTheActionItTightens(t *testing.T) {
	withDeployGrantedToThreeTiers(t)
	path := "/etc/mantlekeep/scopes/example.json"
	layer := ScopeLayer("example",
		map[string]mantlekeep.Role{"service.deploy": mantlekeep.RoleArchitect}, nil)

	notices := PrecedenceNotices(nil, path, layer, Resolve(nil, layer))
	if len(notices) != 1 {
		t.Fatalf("a layer that tightens one document-granted action must produce exactly one "+
			"line; got %d: %v", len(notices), notices)
	}
	line := notices[0]

	for _, must := range []string{path, "service.deploy", "L1-Architect", "TIGHTENS"} {
		if !strings.Contains(line, must) {
			t.Fatalf("the boot line does not contain %q, so a reader cannot act on it "+
				"without already knowing the answer:\n%s", must, line)
		}
	}
	// The roles that LOSE the action are the actionable half: without them the operator knows
	// something changed but not for whom.
	for _, role := range []string{"L2-Operator", "L3-Consumer"} {
		if !strings.Contains(line, role) {
			t.Fatalf("the boot line does not name %q, which the document grants service.deploy "+
				"and this layer now refuses:\n%s", role, line)
		}
	}
	// L1-Architect satisfies the layer, so naming it as refused would send somebody to fix a
	// permission that still works.
	if strings.Contains(line, "L1-Architect no longer") {
		t.Fatalf("the line reports a role that is senior enough as refused:\n%s", line)
	}
	// The wildcard holder is granted everything and is senior to the requirement. Naming it in
	// every line teaches nobody anything and would bury the roles that matter.
	if strings.Contains(line, "L0-SuperAdmin") {
		t.Fatalf("the line names the wildcard holder, which loses nothing here:\n%s", line)
	}
}

// An overlap that changes NO decision is still reported — the operator asked for a layer and is
// entitled to know it was consulted — but it must not claim a tightening that did not happen.
func TestAnOverlapThatRefusesNobodySaysSo(t *testing.T) {
	withDeployGrantedToThreeTiers(t)
	layer := ScopeLayer("example",
		map[string]mantlekeep.Role{"service.deploy": mantlekeep.RoleConsumer}, nil)

	notices := PrecedenceNotices(nil, "scopes/example.json", layer, Resolve(nil, layer))
	if len(notices) != 1 {
		t.Fatalf("expected one line for the overlapping action; got %v", notices)
	}
	if strings.Contains(notices[0], "TIGHTENS") {
		t.Fatalf("every role the document grants service.deploy is senior enough for "+
			"L3-Consumer, so nothing tightened — reporting one sends somebody hunting a "+
			"refusal that will never happen:\n%s", notices[0])
	}
	if !strings.Contains(notices[0], "no decision changes") {
		t.Fatalf("the line should say plainly that nothing changed:\n%s", notices[0])
	}
}

// A layer naming an action no document grants is the ORDINARY case — the cascade's
// long-standing fallback, not a change. Reporting it would drown the lines that matter.
func TestNoNoticeWhenNoDocumentGrantsTheAction(t *testing.T) {
	withDeployGrantedToThreeTiers(t)
	layer := ScopeLayer("example",
		map[string]mantlekeep.Role{"nothing.grants.this": mantlekeep.RoleArchitect}, nil)

	notices := PrecedenceNotices(nil, "scopes/example.json", layer, Resolve(nil, layer))
	if len(notices) != 0 {
		t.Fatalf("an action no document grants is not an overlap and must be silent; got %v",
			notices)
	}
}

// The lockout case, reported whether or not a document is involved: a role the ladder cannot
// rank refuses everyone, so the file that wrote it must be named. ValidateLayers refuses
// startup on this for the base cascade; a per-scope layer is not in that set, so this line is
// the only thing an operator gets.
func TestTheDiagnosticNamesARoleTheLadderCannotRank(t *testing.T) {
	withDeployGrantedToThreeTiers(t)
	layer := ScopeLayer("example",
		map[string]mantlekeep.Role{"anything.at.all": mantlekeep.Role("L1-Architech")}, nil)

	notices := PrecedenceNotices(nil, "scopes/example.json", layer, Resolve(nil, layer))
	if len(notices) != 1 {
		t.Fatalf("a role the ladder cannot rank must be reported; got %v", notices)
	}
	for _, must := range []string{"scopes/example.json", "L1-Architech", "cannot rank"} {
		if !strings.Contains(notices[0], must) {
			t.Fatalf("the line omits %q, so nobody can find the typo:\n%s", must, notices[0])
		}
	}
}

// The ladder is the deployment's OWN vocabulary, so the notice must rank against it. A
// deployment that renamed its tiers would otherwise be told its every binding is unrankable —
// a boot log full of false alarms about roles that work perfectly.
func TestTheDiagnosticRanksAgainstTheDeploymentsOwnLadder(t *testing.T) {
	withDocument(t, map[string]map[string]bool{"Reader": {"service.deploy": true}})
	ladder := RoleLadder{"Owner": 0, "Reader": 1}
	layer := ScopeLayer("example",
		map[string]mantlekeep.Role{"service.deploy": mantlekeep.Role("Owner")}, nil)

	notices := PrecedenceNotices(ladder, "scopes/example.json", layer, Resolve(ladder, layer))
	if len(notices) != 1 {
		t.Fatalf("expected one line about the renamed tiers; got %v", notices)
	}
	if strings.Contains(notices[0], "cannot rank") {
		t.Fatalf("a role this deployment's ladder defines was reported as unrankable — the "+
			"notice is ranking against the built-in default instead:\n%s", notices[0])
	}
	if !strings.Contains(notices[0], "TIGHTENS") || !strings.Contains(notices[0], "Reader") {
		t.Fatalf("Owner is senior to the document's Reader, so Reader loses the action and "+
			"must be named:\n%s", notices[0])
	}
}

// Order must be stable — Go map order is random, and a boot log that reshuffles itself cannot
// be diffed between two deployments that are supposed to be identical.
func TestNoticesComeOutInAStableOrder(t *testing.T) {
	withDocument(t, map[string]map[string]bool{
		"L3-Consumer": {"service.deploy": true, "service.plan": true, "service.reject": true},
	})
	layer := ScopeLayer("example", map[string]mantlekeep.Role{
		"service.deploy": mantlekeep.RoleArchitect,
		"service.plan":   mantlekeep.RoleArchitect,
		"service.reject": mantlekeep.RoleArchitect,
	}, nil)

	first := strings.Join(PrecedenceNotices(nil, "scopes/example.json", layer, Resolve(nil, layer)), "\n")
	if strings.Count(first, "\n") != 2 {
		t.Fatalf("expected three lines to order; got:\n%s", first)
	}
	for attempt := 0; attempt < 20; attempt++ {
		again := strings.Join(PrecedenceNotices(nil, "scopes/example.json", layer, Resolve(nil, layer)), "\n")
		if again != first {
			t.Fatalf("the notices changed order between two calls over the same layer:\n%s\n\n%s",
				first, again)
		}
	}
}

// A layer file may ask for something the cascade does not grant it: a SEALED floor set higher
// up rejects a looser override. The notice must describe the role that DECIDES, and say the
// file's value was rejected — quoting the file here would announce a requirement the engine
// does not have, which is the same class of lie this whole rule exists to remove.
func TestTheDiagnosticSaysWhenASealRejectedTheLayersValue(t *testing.T) {
	withDeployGrantedToThreeTiers(t)
	platform := Layer{
		Name:        "platform",
		ActionRoles: map[string]mantlekeep.Role{"service.deploy": mantlekeep.RoleArchitect},
		Sealed:      []string{"action:service.deploy"},
	}
	scope := ScopeLayer("example",
		map[string]mantlekeep.Role{"service.deploy": mantlekeep.RoleConsumer}, nil)

	notices := PrecedenceNotices(nil, "scopes/example.json", scope, Resolve(nil, platform, scope))
	if len(notices) == 0 {
		t.Fatal("a rejected override must be reported: the file says one thing, the engine " +
			"enforces another, and only the log can tell an operator which")
	}
	rejected := notices[0]
	for _, must := range []string{"scopes/example.json", "SEALED", "L3-Consumer", "L1-Architect"} {
		if !strings.Contains(rejected, must) {
			t.Fatalf("the line omits %q, so it does not say what was asked for, what decides, "+
				"or where to look:\n%s", must, rejected)
		}
	}
	// And the overlap analysis that follows must describe the role that decides — L1-Architect
	// — not the one the file asked for. Reporting against L3-Consumer would claim nothing
	// changed.
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "TIGHTENS") || !strings.Contains(joined, "L2-Operator") {
		t.Fatalf("the overlap must be judged against the role that decides (L1-Architect), "+
			"which refuses the document's L2-Operator and L3-Consumer:\n%s", joined)
	}
}
