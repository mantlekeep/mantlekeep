package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/safeio"
)

// This file is ONE concern: the SCOPE tier of the cascade — a directory of per-scope layer
// files, and the per-scope resolver they wire onto the engine. The platform/team layers and
// their file format live in policyconfig.go.

// scopeLayerFile is a loaded scope layer WITH the file it came from. The path travels because
// the boot diagnostic names it, and "some scope tightened something" is not a diagnostic — it
// is a search. Layer.Name carries only the scope key, which is not a path anyone can open.
type scopeLayerFile struct {
	path  string
	layer policy.Layer
}

// loadScopes reads a DIRECTORY of per-scope layer files (MANTLEKEEP_SCOPE_CONFIG). Each
// <scope>.json is that scope's override layer, same schema as the platform/team layers. A
// scope is the GENERIC tenancy tier — a product maps its project/tenant/app onto it. Returns
// scope→layer-and-path; empty when unset or unreadable.
func loadScopes(envVar string, verbose bool) map[string]scopeLayerFile {
	dir := os.Getenv(envVar)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scope config %s: %v — ignored\n", dir, err)
		return nil
	}
	out := map[string]scopeLayerFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())
		data, err := safeio.ReadConfigFile(path)
		if err != nil {
			continue
		}
		var raw layerFile
		if err := json.Unmarshal(data, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "scope %q: bad JSON — ignored\n", name)
			continue
		}
		out[name] = scopeLayerFile{
			path: path,
			layer: policy.Layer{
				Name:        "scope:" + name,
				ActionRoles: toRoleMap(raw.ActionRoles),
				Sealed:      raw.Sealed,
			},
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
func attachScopes(eng *policy.RBAC, base []policy.Layer, fallback policy.ActionAuthorizer, ladder policy.RoleLadder, verbose bool) *policy.RBAC {
	scopes := loadScopes("MANTLEKEEP_SCOPE_CONFIG", verbose)
	if len(scopes) == 0 {
		return eng
	}
	sr := policy.NewScopeResolver(ladder, base...)
	if fallback != nil {
		sr.WithFallback(fallback)
	}
	for name, scoped := range scopes {
		sr.SetScope(name, scoped.layer)
		if verbose {
			// The scope's own tier, resolved on top of the shared base — the seal is applied
			// here, so this is what the file actually achieved rather than what it asked for.
			cascade := policy.Resolve(ladder, append(append([]policy.Layer{}, base...), scoped.layer)...)
			printPrecedenceNotices(ladder, scoped.path, scoped.layer, cascade)
		}
	}
	if verbose {
		fmt.Printf("policy: per-scope resolution enabled (%d scopes) — known scopes resolve their own tier, others use the base\n", len(scopes))
	}
	return eng.WithScopes(sr)
}
