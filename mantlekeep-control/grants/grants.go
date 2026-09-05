// Package grants holds the SHARED policy grant document: the role→action grants and the
// approval-shaped actions, as ONE data document that both the pure-Go RBAC engine
// (internal/policy) and the OPA adapter (mantlekeep.dev/opa) read. Keeping the product action
// vocabulary HERE — as data — instead of in the engine code is what keeps the core policy
// generic and grep-clean of product names. The two engines can no longer drift because they
// load the identical document.
//
// Config-flexible source: MANTLEKEEP_POLICY_GRANTS may override the embedded default with a file
// path or inline JSON; a DB-backed source drops in behind Load() with no caller change (the
// same seam as the layered-config Source in internal/policy).
package grants

import (
	// Blank import: the //go:embed directive below needs the embed package linked in, but
	// this file references no embed identifier of its own.
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/safeio"
)

// defaultDoc is the embedded default — now EMPTY. The core binary carries NO policy: the platform
// baseline is IT's external MANTLEKEEP_PLATFORM_POLICY and products supply MANTLEKEEP_POLICY_DIR docs, so the
// whole policy is dynamic config, changeable with no recompile. This embed only guarantees Load() a
// well-formed zero value when nothing is configured (fail-closed: no grants ⇒ deny).
//
//go:embed grants.json
var defaultDoc []byte

// EnvOverride is the env var that, when set, replaces the embedded default. Its value is either
// inline JSON (starts with '{') or a path to a JSON file.
const EnvOverride = "MANTLEKEEP_POLICY_GRANTS"

// Grants is the shared policy grant document.
type Grants struct {
	// RoleActions maps a role to the actions it may issue. These are EXPLICIT per-role sets
	// (not a seniority ladder): a role gets exactly the actions listed for it. The generic
	// L0-SuperAdmin wildcard and the promote gate are NOT here — they live in the engine.
	RoleActions map[string][]string `json:"role_actions"`
	// ApprovalActions are the approval-shaped actions an AI may never perform, whatever role it
	// holds. The guardrail logic is generic in the engine; only the action names live here.
	ApprovalActions []string `json:"approval_actions"`
}

// Load reads the grant document and composes the three layers onto it: the embedded (or
// overridden) baseline, then IT's PLATFORM policy — which is exempt from the seal and
// DEFINES it — then each PRODUCT doc, which is subject to it.
func Load() (*Grants, error) {
	g, err := baselineGrants()
	if err != nil {
		return nil, err
	}
	sealed, err := applyPlatformLayer(g)
	if err != nil {
		return nil, err
	}
	if err := applyProductLayers(g, sealed); err != nil {
		return nil, err
	}
	return g, nil
}

// baselineGrants reads the starting document — the MANTLEKEEP_POLICY_GRANTS override if set,
// else the embedded default — as a value the layers above can merge onto. RoleActions is
// guaranteed non-nil: a document that simply omits the key (`{}` is enough) unmarshals it
// as nil, and the platform merge's first write to a nil map panics.
func baselineGrants() (*Grants, error) {
	doc := defaultDoc
	if v := os.Getenv(EnvOverride); v != "" {
		b, err := readOverride(v)
		if err != nil {
			return nil, fmt.Errorf("policy grants override: %w", err)
		}
		doc = b
	}
	var g Grants
	if err := json.Unmarshal(doc, &g); err != nil {
		return nil, fmt.Errorf("policy grants: %w", err)
	}
	if g.RoleActions == nil {
		g.RoleActions = map[string][]string{}
	}
	return &g, nil
}

// applyPlatformLayer merges LAYER 1 — the IT-owned PLATFORM policy
// (MANTLEKEEP_PLATFORM_POLICY) — and returns the SEAL it defines.
//
// The seal is the point: every verb the platform GRANTS is sealed automatically, so a
// product may never grant a platform action even if IT forgot to list it in
// sealed_actions. Fail CLOSED, not open — the seal cannot depend on IT remembering a list.
// sealed_actions then ADDS verbs IT forbids products from granting even though the
// platform itself does not grant them.
//
// Absent ⇒ no platform grants and an empty seal (fail-closed: an action no one is granted
// is denied anyway).
func applyPlatformLayer(g *Grants) (map[string]bool, error) {
	sealed := map[string]bool{}
	plat, err := platformDoc()
	if err != nil {
		return nil, fmt.Errorf("policy grants: %w", err)
	}
	if plat == nil {
		return sealed, nil
	}
	for role, acts := range plat.RoleActions {
		for _, a := range acts {
			sealed[a] = true
		}
		g.RoleActions[role] = unionStrings(g.RoleActions[role], acts)
	}
	g.ApprovalActions = unionStrings(g.ApprovalActions, plat.ApprovalActions)
	for _, a := range plat.SealedActions {
		sealed[a] = true
	}
	return sealed, nil
}

// applyProductLayers merges LAYER 2 — the PRODUCT docs (MANTLEKEEP_POLICY_DIR), which are
// SUBJECT to the seal. A product may add ONLY its own actions; granting a sealed platform
// action FAILS THE LOAD — a policy reaching for platform power is noticed, not silently
// trimmed.
func applyProductLayers(g *Grants, sealed map[string]bool) error {
	docs, err := productDocs()
	if err != nil {
		return fmt.Errorf("policy grants: %w", err)
	}
	for _, d := range docs {
		for role, acts := range d.RoleActions {
			if err := refuseSealedActions(d.Source, role, acts, sealed); err != nil {
				return err
			}
			g.RoleActions[role] = unionStrings(g.RoleActions[role], acts)
		}
		g.ApprovalActions = unionStrings(g.ApprovalActions, d.ApprovalActions)
	}
	return nil
}

// refuseSealedActions is the seal itself, in one place: the refusal a product doc earns by
// granting a verb the platform owns. Named so the rule is one thing a reader can find,
// rather than a condition buried in the merge loop.
func refuseSealedActions(source, role string, acts []string, sealed map[string]bool) error {
	for _, a := range acts {
		if sealed[a] {
			return fmt.Errorf("product policy %q grants sealed platform action %q (role %q) — a product may not grant a platform verb", source, a, role)
		}
	}
	return nil
}

// MustLoad is Load or panic — used where a malformed grant document is a configuration/build
// error, not a runtime condition (the embedded default always parses; an override that does not
// is a misconfiguration the operator wants to fail fast on).
func MustLoad() *Grants {
	g, err := Load()
	if err != nil {
		panic(err)
	}
	return g
}

func readOverride(v string) ([]byte, error) {
	if len(v) > 0 && v[0] == '{' {
		return []byte(v), nil // inline JSON
	}
	return safeio.ReadConfigFile(v) // operator-set file path, read through the validated config door
}

// RoleActionsAny renders the role→action grants as the generic shape an OPA data store expects
// (role → array of action strings), so the OPA adapter reads data.grants.role_actions.
func (g *Grants) RoleActionsAny() map[string]any {
	m := make(map[string]any, len(g.RoleActions))
	for role, acts := range g.RoleActions {
		arr := make([]any, len(acts))
		for i, a := range acts {
			arr[i] = a
		}
		m[role] = arr
	}
	return m
}

// ApprovalActionsAny renders the approval actions for the OPA data store
// (data.grants.approval_actions).
func (g *Grants) ApprovalActionsAny() []any {
	arr := make([]any, len(g.ApprovalActions))
	for i, a := range g.ApprovalActions {
		arr[i] = a
	}
	return arr
}
