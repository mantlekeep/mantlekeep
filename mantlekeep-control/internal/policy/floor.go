package policy

import (
	"strconv"
	"strings"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// The GENERIC attribute-floor evaluator. A product's IT-owned floor is DATA (grants/floors.json,
// action → []FloorRule); this engine dispatches on the rule kind and reads intent.Params by the
// param NAME the data supplies. The core names no product concept — the param names it checks, and
// their allowed values, are strings that live in the data, never as literals in this code. The OPA
// adapter mirrors this in providers/floor.rego, reading the same data.floors, so the two engines
// cannot drift.

// floors() (the lazy, merged floor document — baseline ∪ product docs) is defined in
// grants_load.go. MANTLEKEEP_POLICY_DIR contributes each product's floor; MANTLEKEEP_POLICY_FLOORS may
// override the baseline. A malformed doc fails fast.

// admitFloor evaluates the floor rules for an action against the request params + caller roles.
// It returns (true, reason) to DENY, (false, "") to allow. An action with no floor rules always
// passes — the floor is action-scoped by the data, so the engine stays generic.
func admitFloor(ladder RoleLadder, action string, params map[string]any, roles []mantlekeep.Role) (bool, string) {
	for _, rule := range floors().Floors[action] {
		if reason := evalFloorRule(ladder, rule, params, roles); reason != "" {
			return true, reason
		}
	}
	return false, ""
}

// evalFloorRule returns "" when the rule is satisfied, else a deny reason (the rule's Message,
// sometimes with the offending detail appended). ladder is the deployment's role vocabulary,
// used by the required_role_when rule so a seniority floor honours renamed tiers.
func evalFloorRule(ladder RoleLadder, rule grants.FloorRule, params map[string]any, roles []mantlekeep.Role) string {
	switch rule.Kind {
	case "allowlist":
		// The value must be a listed member. Absent/empty fails closed.
		if !floorContains(rule.Values, floorParamString(params, rule.Param)) {
			return rule.Message
		}
	case "prefix_allowlist":
		// A named value must start with an approved prefix; an empty value skips the rule.
		if v := floorParamString(params, rule.Param); v != "" && !floorHasPrefix(v, rule.Values) {
			return rule.Message
		}
	case "required_pattern_when_in":
		// When another param is in a set, the named value must contain a required pattern; an
		// empty value skips the rule.
		v := floorParamString(params, rule.Param)
		if v != "" && floorContains(rule.WhenIn, floorParamString(params, rule.WhenParam)) &&
			!strings.Contains(v, rule.Pattern) {
			return rule.Message
		}
	case "capped_map":
		// Every entry in the request map must have a configured cap AND be at or below it. An
		// uncapped, NON-STRING, or unparseable entry fails closed — a value sent as a JSON number
		// (not a k8s-quantity string) is refused, matching providers/floor.rego where q() is
		// undefined for a non-string and the entry becomes a violation. Dropping non-strings here
		// would let a numeric value skip its cap.
		for res, raw := range floorParamRawMap(params, rule.Param) {
			cap, ok := rule.Caps[res]
			if !ok {
				return rule.Message + " (" + res + " has no configured cap)"
			}
			want, isString := raw.(string)
			if !isString {
				return rule.Message + " (non-string quantity for " + res + ")"
			}
			le, ok := quantityLessOrEqual(want, cap)
			if !ok {
				return rule.Message + " (unparseable quantity for " + res + ")"
			}
			if !le {
				return rule.Message + " (" + res + "=" + want + " exceeds cap " + cap + ")"
			}
		}
	case "require_approval_when":
		// Never a deny, and stated here rather than left to the default so a reader is not
		// left wondering whether it was forgotten. It is evaluated by approvalGate, AFTER
		// every deny rule — including this loop — has finished.
	case "required_role_when":
		// When a param equals a trigger value, the caller must hold a role at least as senior as
		// the required one (uses the core's generic authority ranking).
		if floorWhenMatches(params, rule.WhenParam, rule.WhenValue) &&
			!ladder.holdsAtLeast(rolesToStrings(roles), mantlekeep.Role(rule.Role)) {
			return rule.Message
		}
	}
	return ""
}

// ── generic k8s-quantity comparison ───────────────────────────────────────────
// A request value and its cap are the same resource, so they need only be comparable to each
// other. parseQuantity maps every k8s quantity to a single float in canonical units (millis,
// binary/decimal suffixes, or bare), so cpu ("8","500m") and memory ("16Gi") each compare within
// their own kind without the engine knowing which is which. Mirrored by q() in providers/floor.rego.

func quantityLessOrEqual(a, b string) (le bool, ok bool) {
	av, ok1 := parseQuantity(a)
	bv, ok2 := parseQuantity(b)
	if !ok1 || !ok2 {
		return false, false
	}
	return av <= bv, true
}

var floorBinFactor = map[string]float64{"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40, "Pi": 1 << 50}
var floorDecFactor = map[string]float64{"K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15}

// parseQuantity parses a k8s quantity to a float: "8" → 8, "500m" → 0.5, "16Gi" → 16*2^30,
// "500M" → 5e8. Binary suffixes (Ki/Mi/Gi/Ti/Pi) are checked first, then milli ("m"), then decimal
// (K/M/G/T/P), then a bare number. An unparseable value returns ok=false (the caller fails closed).
func parseQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "i") && len(s) >= 3 {
		if f, ok := floorBinFactor[s[len(s)-2:]]; ok {
			n, err := strconv.ParseFloat(s[:len(s)-2], 64)
			if err != nil {
				return 0, false
			}
			return n * f, true
		}
		return 0, false
	}
	if strings.HasSuffix(s, "m") {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, false
		}
		return n * 0.001, true
	}
	if f, ok := floorDecFactor[s[len(s)-1:]]; ok {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, false
		}
		return n * f, true
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ── param readers (params arrive as map[string]any off the wire) ──────────────

func floorParamString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	s, _ := p[key].(string)
	return s
}

// floorWhenMatches reports whether params[key] equals the trigger value, tolerating a JSON bool
// true and the string "true" a caller may send (matches the pre-refactor paramBool behaviour).
func floorWhenMatches(p map[string]any, key, want string) bool {
	if p == nil {
		return false
	}
	switch v := p[key].(type) {
	case bool:
		return v && want == "true"
	case string:
		return v == want
	}
	return false
}

// floorParamRawMap returns the request map UNCONVERTED, so capped_map can fail closed on a
// non-string value (a JSON number) rather than silently dropping it.
func floorParamRawMap(p map[string]any, key string) map[string]any {
	out := map[string]any{}
	if p == nil {
		return out
	}
	switch m := p[key].(type) {
	case map[string]any:
		for k, v := range m {
			out[k] = v
		}
	case map[string]string:
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func floorContains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func floorHasPrefix(v string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}

// approvalGate reports whether an otherwise-allowed action needs a second person.
//
// It is the ONE floor rule that does not deny. Everything else in this file can turn an
// allow into a refusal; this turns an allow into "a person must sign this", which is a
// different thing and the only one a requester can act on.
//
// The condition is DATA. The engine still knows no action, environment or role name of its
// own — a deployment declares when approval is required by writing a rule, and the rule is
// matched against the intent's params exactly like every other floor rule.
//
// The loop terminates because the gate is satisfied by a SECOND PARTY: an intent naming a
// requester other than the acting subject passes straight through, so the approver's
// re-submission completes where the original was told to wait. Without that, an approval
// would hit the same rule and open another approval forever.
//
// Role is OPTIONAL. An owning-team gate wants any second colleague, not a more senior one.
// When the data names a role it travels as who to ask — advisory, because authority is the
// door's to resolve from the directory when the approver actually submits.
func approvalGate(action string, requester, subjectID string,
	params map[string]any) (required bool, reason string, approvers []mantlekeep.Role) {

	for _, rule := range floors().Floors[action] {
		if rule.Kind != "require_approval_when" {
			continue
		}
		if !floorWhenMatches(params, rule.WhenParam, rule.WhenValue) {
			continue
		}
		// A second party is present. (requester == subjectID never reaches here: that is the
		// separation-of-duties deny, upstream.)
		if requester != "" && requester != subjectID {
			continue
		}
		message := rule.Message
		if message == "" {
			// Never silent. A refusal with no words sends the requester to find whoever they
			// can, and the next thing they try is the path that does not ask.
			message = "this change requires approval by a second person"
		}
		if rule.Role != "" {
			approvers = []mantlekeep.Role{mantlekeep.Role(rule.Role)}
		}
		return true, message, approvers
	}
	return false, "", nil
}
