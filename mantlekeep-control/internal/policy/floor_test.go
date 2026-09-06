package policy

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// These pin the GENERIC floor evaluator's decisions, rule kind by rule kind. The engine
// names no product concept — every param name and value below comes from the rule DATA,
// which is the property that keeps the core generic. evalFloorRule returns "" to allow
// and a reason to deny.

const denyMessage = "floor refused this request"

func evalWith(rule grants.FloorRule, params map[string]any, roles ...mantlekeep.Role) string {
	return evalFloorRule(DefaultRoleLadder(), rule, params, roles)
}

func TestAllowlistAdmitsOnlyListedValues(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "allowlist", Param: "cluster",
		Values: []string{"blue", "green"}, Message: denyMessage,
	}

	if got := evalWith(rule, map[string]any{"cluster": "blue"}); got != "" {
		t.Errorf("a listed value was denied: %q", got)
	}
	if got := evalWith(rule, map[string]any{"cluster": "red"}); got != denyMessage {
		t.Errorf("an unlisted value returned %q, want the deny message", got)
	}
	// Fails CLOSED: an absent value is not "no opinion", it is not on the list.
	if got := evalWith(rule, map[string]any{}); got != denyMessage {
		t.Errorf("an absent value returned %q, want the deny message (fail closed)", got)
	}
	if got := evalWith(rule, nil); got != denyMessage {
		t.Errorf("nil params returned %q, want the deny message (fail closed)", got)
	}
}

func TestPrefixAllowlistSkipsAnEmptyValueButRefusesAnUnapprovedPrefix(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "prefix_allowlist", Param: "image",
		Values: []string{"registry.internal/", "mirror.internal/"}, Message: denyMessage,
	}

	if got := evalWith(rule, map[string]any{"image": "registry.internal/app:1"}); got != "" {
		t.Errorf("an approved prefix was denied: %q", got)
	}
	if got := evalWith(rule, map[string]any{"image": "docker.io/app:1"}); got != denyMessage {
		t.Errorf("an unapproved prefix returned %q, want the deny message", got)
	}
	// Unlike allowlist, an EMPTY value skips this rule rather than failing closed.
	if got := evalWith(rule, map[string]any{"image": ""}); got != "" {
		t.Errorf("an empty value returned %q, want the rule skipped", got)
	}
	if got := evalWith(rule, map[string]any{}); got != "" {
		t.Errorf("an absent value returned %q, want the rule skipped", got)
	}
}

func TestRequiredPatternAppliesOnlyWhenTheOtherParamIsInTheSet(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "required_pattern_when_in", Param: "tag",
		WhenParam: "env", WhenIn: []string{"prod", "uat"},
		Pattern: "-release", Message: denyMessage,
	}

	if got := evalWith(rule, map[string]any{"env": "prod", "tag": "v1-release"}); got != "" {
		t.Errorf("a tag carrying the pattern was denied: %q", got)
	}
	if got := evalWith(rule, map[string]any{"env": "prod", "tag": "v1-snapshot"}); got != denyMessage {
		t.Errorf("a tag missing the pattern in prod returned %q, want the deny message", got)
	}
	// The trigger param is not in the set → the rule does not apply.
	if got := evalWith(rule, map[string]any{"env": "dev", "tag": "v1-snapshot"}); got != "" {
		t.Errorf("the rule fired outside its trigger set: %q", got)
	}
	// An empty value skips the rule.
	if got := evalWith(rule, map[string]any{"env": "prod", "tag": ""}); got != "" {
		t.Errorf("an empty value returned %q, want the rule skipped", got)
	}
}

// capped_map is the rule with the most ways to fail, and every one of them fails CLOSED —
// an entry the data did not cap, or a value the engine cannot compare, is a refusal.
func TestCappedMapRefusesEveryUncomparableOrOversizedEntry(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "capped_map", Param: "resources",
		Caps: map[string]string{"cpu": "4", "memory": "16Gi"}, Message: denyMessage,
	}

	within := map[string]any{"resources": map[string]any{"cpu": "2", "memory": "8Gi"}}
	if got := evalWith(rule, within); got != "" {
		t.Errorf("a request inside every cap was denied: %q", got)
	}

	// At the cap is allowed — the comparison is <=, not <.
	atCap := map[string]any{"resources": map[string]any{"cpu": "4", "memory": "16Gi"}}
	if got := evalWith(rule, atCap); got != "" {
		t.Errorf("a request exactly at the cap was denied: %q", got)
	}

	cases := []struct {
		name    string
		entries map[string]any
		want    string
	}{
		{"over the cap", map[string]any{"cpu": "8"}, denyMessage + " (cpu=8 exceeds cap 4)"},
		{"no configured cap", map[string]any{"gpu": "1"}, denyMessage + " (gpu has no configured cap)"},
		{"a JSON number, not a quantity string", map[string]any{"cpu": 2}, denyMessage + " (non-string quantity for cpu)"},
		{"an unparseable quantity", map[string]any{"cpu": "loads"}, denyMessage + " (unparseable quantity for cpu)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evalWith(rule, map[string]any{"resources": c.entries})
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// A map[string]string arrives from some callers instead of map[string]any; both shapes
// must be capped, or one of them would skip the floor entirely.
func TestCappedMapReadsBothMapShapes(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "capped_map", Param: "resources",
		Caps: map[string]string{"cpu": "4"}, Message: denyMessage,
	}
	params := map[string]any{"resources": map[string]string{"cpu": "8"}}
	if got := evalWith(rule, params); got != denyMessage+" (cpu=8 exceeds cap 4)" {
		t.Errorf("a map[string]string entry was not capped: %q", got)
	}
}

// The one rule that is never a deny. It is answered by approvalGate, AFTER every deny
// rule has run; evaluating it here must stay a no-op.
func TestRequireApprovalWhenIsNeverADeny(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "require_approval_when", WhenParam: "prod", WhenValue: "true",
		Message: denyMessage,
	}
	if got := evalWith(rule, map[string]any{"prod": "true"}); got != "" {
		t.Errorf("the approval rule denied (%q) — it must only ever ask for a signature", got)
	}
}

func TestRequiredRoleWhenChecksSeniorityAgainstTheLadder(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "required_role_when", WhenParam: "env", WhenValue: "prod",
		Role: string(mantlekeep.RoleOperator), Message: denyMessage,
	}
	prod := map[string]any{"env": "prod"}

	if got := evalWith(rule, prod, mantlekeep.RoleOperator); got != "" {
		t.Errorf("the exact required role was denied: %q", got)
	}
	if got := evalWith(rule, prod, mantlekeep.RoleArchitect); got != "" {
		t.Errorf("a MORE senior role was denied: %q", got)
	}
	if got := evalWith(rule, prod, mantlekeep.RoleConsumer); got != denyMessage {
		t.Errorf("a junior role returned %q, want the deny message", got)
	}
	if got := evalWith(rule, prod); got != denyMessage {
		t.Errorf("a caller with no roles returned %q, want the deny message", got)
	}
	// Outside the trigger, seniority is not asked for at all.
	if got := evalWith(rule, map[string]any{"env": "dev"}, mantlekeep.RoleConsumer); got != "" {
		t.Errorf("the seniority rule fired outside its trigger: %q", got)
	}
}

// A caller may send a JSON bool where the data names the string "true".
func TestRequiredRoleWhenAcceptsABoolTrigger(t *testing.T) {
	rule := grants.FloorRule{
		Kind: "required_role_when", WhenParam: "prod", WhenValue: "true",
		Role: string(mantlekeep.RoleOperator), Message: denyMessage,
	}
	if got := evalWith(rule, map[string]any{"prod": true}, mantlekeep.RoleConsumer); got != denyMessage {
		t.Errorf("a JSON bool trigger did not fire the rule: %q", got)
	}
	if got := evalWith(rule, map[string]any{"prod": false}, mantlekeep.RoleConsumer); got != "" {
		t.Errorf("a false bool fired the rule: %q", got)
	}
}

// An unknown kind is ignored rather than treated as a deny: the data may name a rule a
// newer engine understands, and an old binary must not refuse everything it has not met.
func TestUnknownRuleKindIsIgnored(t *testing.T) {
	rule := grants.FloorRule{Kind: "not_a_rule_this_engine_knows", Message: denyMessage}
	if got := evalWith(rule, map[string]any{"anything": "x"}); got != "" {
		t.Errorf("an unknown rule kind returned %q, want it ignored", got)
	}
}

func TestParseQuantityCoversEachSuffixFamily(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"8", 8, true},
		{"500m", 0.5, true},
		{"16Gi", 16 * 1024 * 1024 * 1024, true},
		{"500M", 5e8, true},
		{"1Ki", 1024, true},
		{"", 0, false},
		{"loads", 0, false},
		{"12Xi", 0, false},
	}
	for _, c := range cases {
		got, ok := parseQuantity(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseQuantity(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Comparison is within one resource kind, so cpu and memory each compare correctly
// without the engine knowing which is which.
func TestQuantityLessOrEqualComparesWithinAKind(t *testing.T) {
	if le, ok := quantityLessOrEqual("500m", "1"); !ok || !le {
		t.Errorf(`quantityLessOrEqual("500m","1") = (%v,%v), want (true,true)`, le, ok)
	}
	if le, ok := quantityLessOrEqual("32Gi", "16Gi"); !ok || le {
		t.Errorf(`quantityLessOrEqual("32Gi","16Gi") = (%v,%v), want (false,true)`, le, ok)
	}
	if _, ok := quantityLessOrEqual("nonsense", "1"); ok {
		t.Error("an unparseable value reported a usable comparison")
	}
}
