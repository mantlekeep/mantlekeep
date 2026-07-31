package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/internal/policy"
)

// layerFile is the on-disk shape of a policy config layer (platform or team). It is
// plain JSON — no new dependency — the same env-config style the rest of MantleKeep uses.
// It carries action→role bindings and the sealed keys a lower layer may only tighten;
// env-gating of an action is a product's floor DATA (grants/floors.json), not a layer key.
//
//	{
//	  "actionRoles": { "service.deploy": "L1-Architect" },
//	  "sealed":      [ "action:service.deploy" ]
//	}
type layerFile struct {
	ActionRoles map[string]string `json:"actionRoles"`
	Sealed      []string          `json:"sealed"`
}

// loadLayer reads a policy config layer from the file named by envVar. Returns
// (zero, false) when the var is unset. A sealed key set HERE binds every layer applied
// after it (see policy.Resolve): the platform layer's seals are the floor a team layer
// cannot loosen.
//
// verbose controls the "layer loaded" log line: true at boot (log once), false on the
// hot-reload watcher's poll path (silent, so a poll every N seconds does not spam). A
// bad-file / bad-JSON warning is ALWAYS emitted — a broken config the operator must see.
func loadLayer(envVar, name string, verbose bool) (policy.Layer, bool) {
	path := os.Getenv(envVar)
	if path == "" {
		return policy.Layer{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy layer %s (%s): %v — ignored\n", name, path, err)
		return policy.Layer{}, false
	}
	var raw layerFile
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "policy layer %s (%s): bad JSON: %v — ignored\n", name, path, err)
		return policy.Layer{}, false
	}
	layer := policy.Layer{
		Name:        name + ":" + filepath.Base(path),
		ActionRoles: toRoleMap(raw.ActionRoles),
		Sealed:      raw.Sealed,
	}
	if verbose {
		fmt.Printf("policy: layer %q loaded (%d action, %d sealed)\n",
			layer.Name, len(layer.ActionRoles), len(layer.Sealed))
	}
	return layer, true
}

// NOTE: a product's attribute floor is now GENERIC DATA — a list of typed rules per action in the
// shared floor document (grants/floors.json, MANTLEKEEP_POLICY_FLOORS override), applied by the generic
// evaluator in internal/policy (floor.go). The core imports no product and this generic config
// loader holds no product-specific knowledge.

func toRoleMap(in map[string]string) map[string]mantlekeep.Role {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]mantlekeep.Role, len(in))
	for k, v := range in {
		out[k] = mantlekeep.Role(v)
	}
	return out
}

// loadScopes reads a DIRECTORY of per-scope layer files (MANTLEKEEP_SCOPE_CONFIG). Each
// <scope>.json is that scope's override layer, same schema as the platform/team layers. A
// scope is the GENERIC tenancy tier — the SDLC product maps its "project" onto it; other
// products use a tenant/app/namespace. Returns scope→Layer; empty when unset or unreadable.
func loadScopes(envVar string, verbose bool) map[string]policy.Layer {
	dir := os.Getenv(envVar)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scope config %s: %v — ignored\n", dir, err)
		return nil
	}
	out := map[string]policy.Layer{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var raw layerFile
		if err := json.Unmarshal(data, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "scope %q: bad JSON — ignored\n", name)
			continue
		}
		out[name] = policy.Layer{
			Name:        "scope:" + name,
			ActionRoles: toRoleMap(raw.ActionRoles),
			Sealed:      raw.Sealed,
		}
		if verbose {
			fmt.Printf("policy: scope layer %q loaded (%d action, %d sealed)\n",
				name, len(raw.ActionRoles), len(raw.Sealed))
		}
	}
	return out
}

// attachScopes wires per-scope resolution onto an engine when MANTLEKEEP_SCOPE_CONFIG defines
// any scope layers. base is the shared cascade (default→platform→team); fallback is the
// product registry, re-attached so per-scope resolutions keep runtime-added products' RunAs.
// No scopes configured → the engine is returned untouched (the default path is unchanged).
func attachScopes(eng *policy.RBAC, base []policy.Layer, fallback policy.ActionAuthorizer, verbose bool) *policy.RBAC {
	scopes := loadScopes("MANTLEKEEP_SCOPE_CONFIG", verbose)
	if len(scopes) == 0 {
		return eng
	}
	sr := policy.NewScopeResolver(base...)
	if fallback != nil {
		sr.WithFallback(fallback)
	}
	for name, l := range scopes {
		sr.SetScope(name, l)
	}
	if verbose {
		fmt.Printf("policy: per-scope resolution enabled (%d scopes) — known scopes resolve their own tier, others use the base\n", len(scopes))
	}
	return eng.WithScopes(sr)
}
