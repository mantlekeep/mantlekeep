package policy

import (
	"fmt"
	"sort"
	"strings"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// The BOOT DIAGNOSTIC for the precedence rule in precedence.go.
//
// A layer that names an action a grant document also grants DECIDES that action, and
// document-granted roles that are not senior enough are refused. That is the intended rule,
// but it is a rule whose effect is invisible in every file involved: the layer looks the same,
// the document looks the same, and the only evidence is a refusal at 3am in an environment
// nobody changed. So the door says it out loud, once, at boot, naming the FILE and the ACTION
// — the two things an operator needs in order to open something.
//
// It only reports; it never refuses to start. Turning a config overlap into an outage is a
// worse trade than a loud line, and the point is that the first deployment to meet this
// behaviour can READ it, not that it survives it. The errors that DO refuse startup are
// elsewhere on purpose: a malformed layer file fails in the loader, and a layer binding an
// action to a role the ladder never defined fails in ValidateLayers.
//
// It lives beside the rule rather than in the loader so the sentence an operator reads and the
// code that produces the refusal cannot drift apart — and so the ranking and the merged
// document are read from the engine's own state instead of re-derived by a surface.

// PrecedenceNotices returns what one loaded layer CHANGES about the actions it names: one line
// per action, in a stable order (Go map order is random, and a diagnostic that reshuffles
// itself cannot be diffed between two deployments). Empty when the layer changes nothing.
//
// ladder is the deployment's role vocabulary, so the notice ranks by the same table the
// decision will — a deployment that renamed its tiers must not be told about roles it does not
// use. An empty ladder falls back to the built-in default, exactly as Resolve does.
//
// path is the file the layer was read from, quoted verbatim into every line, because "some
// layer tightened something" is not a diagnostic — it is a search.
//
// cascade is the RESOLVED cascade up to and including this layer, and it is required rather
// than optional: a layer's own value is what the file ASKED for, not necessarily what decides.
// A sealed floor set higher up rejects a looser override, and a diagnostic that quoted the file
// would then announce a requirement the engine does not have — which is the same class of lie
// this whole rule exists to remove. Where the two differ, the notice says both.
//
// The caller prints these. Returning strings rather than printing here keeps the engine free
// of an opinion about where a deployment's logs go, and lets a test assert the SENTENCE: a
// diagnostic checked only by "it printed something" is how a message that names no file
// survives to production.
func PrecedenceNotices(ladder RoleLadder, path string, layer Layer, cascade ActionAuthorizer) []string {
	if len(ladder) == 0 {
		ladder = DefaultRoleLadder()
	}
	var notices []string
	for _, action := range sortedLayerActions(layer.ActionRoles) {
		asked := layer.ActionRoles[action]
		need := asked
		if cascade != nil {
			if resolved, ok := cascade.RequiredRole(action); ok {
				need = resolved
			}
		}
		if need != asked {
			notices = append(notices, fmt.Sprintf(
				"policy: layer %s asks for role %s on action %q, but a SEALED floor set higher "+
					"in the cascade keeps it at %s — the override was rejected, not applied. "+
					"What follows describes %s, the role that actually decides.",
				path, asked, action, need, need))
		}

		// A role the ladder cannot ORDER refuses everyone, the wildcard holder included. That
		// is almost always a typo, and it is the one way this rule can lock a deployment out
		// of its own action, so it is reported whether or not a document grants the action.
		// ValidateLayers already refuses startup on this for the base cascade; a per-scope
		// layer is not in that set, which is exactly where this line still earns its keep.
		if _, ranked := ladder[string(need)]; !ranked {
			notices = append(notices, fmt.Sprintf(
				"policy: layer %s names required role %q for action %q, which this "+
					"deployment's role ladder cannot rank — that role refuses EVERY subject, "+
					"including the wildcard holder. Fix the role name in that file.",
				path, need, action))
			continue
		}

		granted := documentGrants(action)
		if len(granted) == 0 {
			continue // no document says anything about this action; nothing overlaps
		}
		refused := rolesRefusedBy(ladder, action, need)
		if len(refused) == 0 {
			notices = append(notices, fmt.Sprintf(
				"policy: layer %s decides action %q (requires %s) — a grant document also "+
					"grants it to %s, all of which are senior enough, so no decision changes.",
				path, action, need, strings.Join(granted, ", ")))
			continue
		}
		notices = append(notices, fmt.Sprintf(
			"policy: layer %s TIGHTENS action %q — it now requires %s, and the grant "+
				"document's %s no longer suffice. Those roles could perform %q and can no "+
				"longer. The layer decides; the document only permits.",
			path, action, need, strings.Join(refused, ", "), action))
	}
	return notices
}

// documentGrants returns the roles a grant document gives this action, sorted, EXCLUDING any
// role holding the wildcard — it grants everything, so naming it in every line teaches nobody
// anything and would bury the roles that matter.
func documentGrants(action string) []string {
	held := map[string]bool{}
	for role, granted := range roleActions() {
		if granted["*"] {
			continue
		}
		if granted[action] {
			held[role] = true
		}
	}
	return sortedNames(held)
}

// rolesRefusedBy returns the document-granted roles the required role now REFUSES —
// granted, but not senior enough — sorted. This is the BITE of an overlap: empty means the
// layer names the action but changes nobody's answer, and non-empty is the exact list of roles
// that could perform the action before the layer was consulted and cannot now.
func rolesRefusedBy(ladder RoleLadder, action string, need mantlekeep.Role) []string {
	refused := map[string]bool{}
	for _, role := range documentGrants(action) {
		if !ladder.holdsAtLeast([]string{role}, need) {
			refused[role] = true
		}
	}
	return sortedNames(refused)
}

func sortedLayerActions(actionRoles map[string]mantlekeep.Role) []string {
	out := make([]string, 0, len(actionRoles))
	for action := range actionRoles {
		out = append(out, action)
	}
	sort.Strings(out)
	return out
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
