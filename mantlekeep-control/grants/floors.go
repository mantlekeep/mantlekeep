package grants

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

// This file holds the SHARED attribute-FLOOR document: the per-action admission rules a product's
// IT-owned floor is expressed as, as ONE data document that both the pure-Go engine
// (internal/policy) and the OPA adapter (mantlekeep.dev/opa) read. Keeping the floor as GENERIC DATA —
// a list of typed rules per action — instead of product-specific code is what lets the core enforce
// a product's attribute floor while naming no product concept (cluster, image, resource). The two
// engines cannot drift because they load the identical document.
//
// Config-flexible source: MANTLEKEEP_POLICY_FLOORS may override the embedded default with a file path
// or inline JSON (the IT-owned floor); a DB-backed source drops in behind LoadFloors() unchanged.

//go:embed floors.json
var defaultFloorsDoc []byte

// FloorsEnvOverride is the env var that, when set, replaces the embedded default floor with an
// inline JSON document (starts with '{') or a path to a JSON file.
const FloorsEnvOverride = "MANTLEKEEP_POLICY_FLOORS"

// FloorRule is ONE generic attribute constraint on an action's params. The engine dispatches on
// Kind; every other field is data the rule kind reads. The core names no product concept — the
// param names (e.g. "clusterId") and values live HERE, as data.
//
// Kinds:
//   - allowlist                — params[Param] must be one of Values (absent/empty ⇒ deny)
//   - prefix_allowlist         — params[Param], if non-empty, must start with one of Values
//   - required_pattern_when_in — when params[WhenParam] ∈ WhenIn, params[Param] must contain Pattern
//   - capped_map               — every entry of the params[Param] map must have a Caps entry and be ≤ it
//   - required_role_when       — when params[WhenParam] == WhenValue, the caller must hold ≥ Role
type FloorRule struct {
	Kind      string            `json:"kind"`
	Param     string            `json:"param,omitempty"`
	Values    []string          `json:"values,omitempty"`
	Caps      map[string]string `json:"caps,omitempty"`
	WhenParam string            `json:"whenParam,omitempty"`
	WhenIn    []string          `json:"whenIn,omitempty"`
	WhenValue string            `json:"whenValue,omitempty"`
	Pattern   string            `json:"pattern,omitempty"`
	Role      string            `json:"role,omitempty"`
	Message   string            `json:"message,omitempty"`
}

// Floors maps an action to the ordered list of floor rules it must clear.
type Floors struct {
	Floors map[string][]FloorRule `json:"floors"`
}

// LoadFloors reads the floor document: the MANTLEKEEP_POLICY_FLOORS override if set, else the embedded
// default.
func LoadFloors() (*Floors, error) {
	doc := defaultFloorsDoc
	if v := os.Getenv(FloorsEnvOverride); v != "" {
		b, err := readOverride(v)
		if err != nil {
			return nil, fmt.Errorf("policy floors override: %w", err)
		}
		doc = b
	}
	var f Floors
	if err := json.Unmarshal(doc, &f); err != nil {
		return nil, fmt.Errorf("policy floors: %w", err)
	}
	if f.Floors == nil {
		f.Floors = map[string][]FloorRule{}
	}
	// The IT PLATFORM doc may carry floors too (append), then each PRODUCT doc's floors append on top.
	// Floors are APPEND-ONLY at every layer: a product can only ADD rules (tighten), never loosen — so
	// no seal is needed here (more rules ⇒ stricter). A product's attribute floor lives in ITS doc.
	if plat, err := platformDoc(); err != nil {
		return nil, fmt.Errorf("policy floors: %w", err)
	} else if plat != nil {
		for action, rules := range plat.Floors {
			f.Floors[action] = append(f.Floors[action], rules...)
		}
	}
	docs, err := productDocs()
	if err != nil {
		return nil, fmt.Errorf("policy floors: %w", err)
	}
	for _, d := range docs {
		for action, rules := range d.Floors {
			f.Floors[action] = append(f.Floors[action], rules...)
		}
	}
	return &f, nil
}

// MustLoadFloors is LoadFloors or panic — a malformed embedded default (or a bad override) is a
// configuration error worth failing fast on, exactly like MustLoad for grants.
func MustLoadFloors() *Floors {
	f, err := LoadFloors()
	if err != nil {
		panic(err)
	}
	return f
}

// FloorsAny renders the floors as the generic shape an OPA data store expects (action → array of
// rule objects), so the OPA adapter reads data.floors[action] with the same fields the Go engine
// evaluates.
func (f *Floors) FloorsAny() map[string]any {
	out := make(map[string]any, len(f.Floors))
	for action, rules := range f.Floors {
		arr := make([]any, len(rules))
		for i, r := range rules {
			arr[i] = ruleAny(r)
		}
		out[action] = arr
	}
	return out
}

func ruleAny(r FloorRule) map[string]any {
	m := map[string]any{"kind": r.Kind}
	if r.Param != "" {
		m["param"] = r.Param
	}
	if len(r.Values) > 0 {
		m["values"] = strsAny(r.Values)
	}
	if len(r.Caps) > 0 {
		caps := make(map[string]any, len(r.Caps))
		for k, v := range r.Caps {
			caps[k] = v
		}
		m["caps"] = caps
	}
	if r.WhenParam != "" {
		m["whenParam"] = r.WhenParam
	}
	if len(r.WhenIn) > 0 {
		m["whenIn"] = strsAny(r.WhenIn)
	}
	if r.WhenValue != "" {
		m["whenValue"] = r.WhenValue
	}
	if r.Pattern != "" {
		m["pattern"] = r.Pattern
	}
	if r.Role != "" {
		m["role"] = r.Role
	}
	if r.Message != "" {
		m["message"] = r.Message
	}
	return m
}

func strsAny(s []string) []any {
	a := make([]any, len(s))
	for i, v := range s {
		a[i] = v
	}
	return a
}
