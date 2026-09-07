package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/safeio"
)

// layerFile is the on-disk shape of a policy config layer (platform or team). It is
// plain JSON — no new dependency — the same env-config style the rest of MantleKeep uses.
// It carries action→role bindings and the sealed keys a lower layer may only tighten;
// env-gating of an action is a product's floor DATA (grants/floors.json), not a layer key.
//
//	{
//	  "roles":       { "L0-SuperAdmin": 0, "L1-Super-Admin": 1, "L2-Engineer": 2 },
//	  "actionRoles": { "service.deploy": "L1-Super-Admin" },
//	  "sealed":      [ "action:service.deploy" ]
//	}
//
// roles, when present, declares the deployment's role vocabulary (name→authority rank, lower =
// more senior). It is the ONE place a deployment renames the built-in tiers; the FIRST layer that sets
// it wins (see policy.LadderFrom). Omit it and the built-in five tiers apply unchanged.
type layerFile struct {
	Roles       map[string]int    `json:"roles"`
	ActionRoles map[string]string `json:"actionRoles"`
	Sealed      []string          `json:"sealed"`
}

// loadLayer reads a policy config layer from the file named by envVar. It returns
// (zero, false, nil) when the var is UNSET — no config is a legitimate choice, the defaults
// apply. But when the var IS set, an unreadable / malformed / unknown-key file is a HARD
// ERROR (zero, false, err), not a silent drop: a governance engine must never run on a law
// its operator declared but that failed to load, because a dropped layer means policy weaker
// than what was written. The caller propagates the error to a fail-closed refuse-to-start.
//
// A sealed key set HERE binds every layer applied after it (see policy.Resolve): the platform
// layer's seals are the floor a team layer cannot loosen.
//
// verbose controls the per-layer fingerprint log line: true at boot (log once), false on the
// hot-reload watcher's poll path (silent, so a poll every N seconds does not spam).
func loadLayer(envVar, name string, verbose bool) (policy.Layer, bool, error) {
	path := os.Getenv(envVar)
	if path == "" {
		return policy.Layer{}, false, nil
	}
	data, err := safeio.ReadConfigFile(path)
	if err != nil {
		return policy.Layer{}, false, fmt.Errorf("policy layer %s (%s): %w", name, path, err)
	}
	raw, err := decodeLayerFile(data)
	if err != nil {
		return policy.Layer{}, false, fmt.Errorf("policy layer %s (%s): %w", name, path, err)
	}
	layer := policy.Layer{
		Name:        name + ":" + filepath.Base(path),
		ActionRoles: toRoleMap(raw.ActionRoles),
		Sealed:      raw.Sealed,
		Roles:       raw.Roles,
	}
	if verbose {
		// Fingerprint the RAW file bytes and log it so a config change is VISIBLE: an operator
		// (or an auditor reading the boot log) can confirm WHICH file content is live from the
		// hash, without trusting a path or a mtime. The bytes are the same ones parsed above.
		sum := sha256.Sum256(data)
		fmt.Printf("policy: layer %q loaded — sha256=%s… (%d actions, %d sealed, %d roles)\n",
			name, short(hex.EncodeToString(sum[:])), len(layer.ActionRoles), len(layer.Sealed), len(layer.Roles))
	}
	return layer, true, nil
}

// decodeLayerFile parses a layer file and REJECTS an unknown key. DisallowUnknownFields turns a
// typo — "actionRole" for "actionRoles", "seal" for "sealed" — into an error instead of a
// silently ignored field, which would otherwise leave the intended binding or seal missing and
// the policy quietly weaker than the author believed.
func decodeLayerFile(data []byte) (layerFile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw layerFile
	if err := decoder.Decode(&raw); err != nil {
		return layerFile{}, fmt.Errorf("invalid layer config: %w", err)
	}
	return raw, nil
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

// currentLayers reads the env-configured cascade (least specific first). It is called
// both at boot (verbose=true, logs each layer once) and on every watcher poll
// (verbose=false, silent) — the single source of truth for "what are the layers now".
func currentLayers(verbose bool) ([]policy.Layer, error) {
	layers := []policy.Layer{policy.DefaultLayer()}
	// Paths run parallel to layers[1:]. They travel only so the boot diagnostic can NAME the
	// file it is talking about; Layer.Name carries a label, not a path a person can open.
	var paths []string

	// Platform layer FIRST (so its seals bind the team layer that follows), then team. A
	// SET-but-invalid layer is a hard error that propagates — the cascade is never built from
	// a partially-loaded config.
	if l, ok, err := loadLayer("MANTLEKEEP_PLATFORM_CONFIG", "platform", verbose); err != nil {
		return nil, err
	} else if ok {
		layers = append(layers, l)
		paths = append(paths, os.Getenv("MANTLEKEEP_PLATFORM_CONFIG"))
	}
	if l, ok, err := loadLayer("MANTLEKEEP_TEAM_CONFIG", "team", verbose); err != nil {
		return nil, err
	} else if ok {
		layers = append(layers, l)
		paths = append(paths, os.Getenv("MANTLEKEEP_TEAM_CONFIG"))
	}
	if verbose {
		reportPrecedence(layers, paths)
	}
	return layers, nil
}

// reportPrecedence emits the boot diagnostic for each loaded layer — the actions where that
// layer now DECIDES an action a grant document also grants (see policy.PrecedenceNotices).
//
// Reported AFTER the cascade is assembled, not as each file is read: a layer's own value is
// what it asked for, and only the resolved cascade knows whether a sealed floor above it let
// that value through.
//
// A layer set that does not validate is about to refuse startup (the caller runs
// ValidateLayers and exits), so it is passed over in silence rather than described — notices
// about a law that will not run are noise in front of the error that matters.
func reportPrecedence(layers []policy.Layer, paths []string) {
	ladder := policy.LadderFrom(layers...)
	if policy.ValidateLayers(ladder, layers...) != nil {
		return
	}
	for i, path := range paths {
		printPrecedenceNotices(ladder, path, layers[i+1], policy.Resolve(ladder, layers[:i+2]...))
	}
}

// printPrecedenceNotices reports, at BOOT only, every action where this layer now overrides a
// grant document — see policy.PrecedenceNotices for what it says and why it says it.
//
// To stderr, where the other policy-config warnings go: this is diagnostic output about a
// governance change, not part of the normal "loaded" narration. The hot-reload watcher's poll
// path passes verbose=false and never reaches here, so a two-second poll cannot spam it.
func printPrecedenceNotices(ladder policy.RoleLadder, path string, layer policy.Layer, cascade policy.ActionAuthorizer) {
	for _, notice := range policy.PrecedenceNotices(ladder, path, layer, cascade) {
		fmt.Fprintln(os.Stderr, notice)
	}
}
