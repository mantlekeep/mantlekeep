package policy

import (
	"fmt"
	"strings"
)

// This file is ONE concern: the SEMANTIC cross-check of a loaded layer set against the
// resolved role vocabulary. Structural validity (well-formed JSON, no unknown keys) is the
// loader's job; here we catch the mistake JSON alone cannot see — a layer that binds an action
// to, or seals it under, a role name the ladder never defined.
//
// WHY this is fail-CLOSED and not a warning: the ladder is now config-driven (a deployment
// renames its tiers in a layer's `roles` map). A single typo — "L1-Super-Admn" for
// "L1-Super-Admin" — produces a binding whose required role is unknown to the ladder. At
// request time holdsAtLeast can never satisfy an unknown role, so the action would silently
// DENY for everyone, or (worse, on a loosened path) resolve unpredictably. A governance engine
// must refuse to start on a law it cannot honour, naming the exact offender, rather than run and
// surprise an operator on the first request.

// ValidateLayers reports the first semantic error in the layer set, ranked against ladder — the
// resolved role vocabulary (LadderFrom(layers...), which is the built-in default when no layer
// declares a `roles` map). It checks two things per layer:
//
//   - every actionRoles VALUE (a required role) is a role the ladder defines; and
//   - every sealed entry is a well-formed "action:<name>" reference (a role name or a bare
//     action mistakenly placed in `sealed` is a config error, not a silent no-op seal).
//
// A nil error means the layers are safe to resolve and govern on. The error NAMES the offending
// action and unknown role so an operator fixes the exact typo.
func ValidateLayers(ladder RoleLadder, layers ...Layer) error {
	if len(ladder) == 0 {
		ladder = DefaultRoleLadder()
	}
	for _, layer := range layers {
		for action, required := range layer.ActionRoles {
			if _, defined := ladder[string(required)]; !defined {
				return fmt.Errorf(
					"policy layer %q: action %q is bound to role %q, which the role ladder does not define",
					layer.Name, action, string(required))
			}
		}
		for _, sealed := range layer.Sealed {
			if err := validateSealRef(layer.Name, sealed); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateSealRef rejects a malformed seal key. A seal locks an ACTION as a floor, so the only
// valid form is "action:<name>" with a non-empty name. Catching a bare or mis-typed seal here
// stops a silent no-op floor — a seal an operator believed was protecting an action but that
// matched nothing.
func validateSealRef(layerName, sealed string) error {
	const prefix = "action:"
	name, ok := strings.CutPrefix(sealed, prefix)
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf(
			"policy layer %q: sealed entry %q is malformed — expected %q<action>",
			layerName, sealed, prefix)
	}
	return nil
}
