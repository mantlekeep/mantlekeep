package grants

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/safeio"
)

// PolicyDirEnv names a list (OS path-list separated) of PRODUCT policy docs — files, or directories
// whose *.json files are each a doc — merged ON TOP of the core's generic/platform baseline.
//
// This is how a product ships its OWN policy without the core repo carrying it: the core is a
// generic engine + a platform baseline (grants.json / floors.json here), and each PRODUCT
// contributes a doc {role_actions?, approval_actions?, floors?}. Load()/LoadFloors() UNION the
// baseline with every product doc, so the composed system's policy equals baseline ∪ products. A
// DB-backed registry drops in behind productDocs() with no caller change.
const PolicyDirEnv = "MANTLEKEEP_POLICY_DIR"

// PlatformPolicyEnv names IT's PLATFORM policy doc — the cross-cutting governance verbs' grants +
// approval actions + the sealed_actions list. It is the SEALING layer: EXEMPT from the seal, and the
// only doc whose sealed_actions are honored. Unset ⇒ no platform grants (fail-closed: an action no
// one is granted is denied). The core binary embeds NO baseline — this external, IT-owned file
// supplies it, changeable with no recompile.
const PlatformPolicyEnv = "MANTLEKEEP_PLATFORM_POLICY"

// productDoc is one contribution to the policy: role grants, approval actions, floors, and (platform
// doc only) the sealed action list.
type productDoc struct {
	RoleActions     map[string][]string    `json:"role_actions"`
	ApprovalActions []string               `json:"approval_actions"`
	Floors          map[string][]FloorRule `json:"floors"`
	SealedActions   []string               `json:"sealed_actions"` // PLATFORM doc only — the seal
	Source          string                 `json:"-"`              // file path, for error messages
}

// platformDoc reads the IT-owned platform policy named by MANTLEKEEP_PLATFORM_POLICY (a file path or
// inline JSON). Returns nil when unset. It carries the sealed_actions the product-doc merge enforces.
func platformDoc() (*productDoc, error) {
	v := os.Getenv(PlatformPolicyEnv)
	if v == "" {
		return nil, nil
	}
	b, err := readOverride(v)
	if err != nil {
		return nil, fmt.Errorf("platform policy %q: %w", v, err)
	}
	var d productDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("platform policy %q: %w", v, err)
	}
	d.Source = v
	return &d, nil
}

// productDocs reads and parses every policy doc named by MANTLEKEEP_POLICY_DIR (unset ⇒ none, the core
// baseline stands alone). Order is deterministic (sorted) so a merge is reproducible.
func productDocs() ([]productDoc, error) {
	v := os.Getenv(PolicyDirEnv)
	if v == "" {
		return nil, nil
	}
	var files []string
	for _, p := range strings.Split(v, string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		info, err := safeio.StatConfigPath(p)
		if err != nil {
			return nil, fmt.Errorf("policy source %q: %w", p, err)
		}
		if info.IsDir() {
			ents, err := os.ReadDir(p)
			if err != nil {
				return nil, err
			}
			for _, e := range ents {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
		} else {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	docs := make([]productDoc, 0, len(files))
	for _, f := range files {
		b, err := safeio.ReadConfigFile(f)
		if err != nil {
			return nil, err
		}
		var d productDoc
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, fmt.Errorf("policy doc %q: %w", f, err)
		}
		d.Source = f
		docs = append(docs, d)
	}
	return docs, nil
}

// unionStrings returns a ∪ b, preserving a's order then b's new entries, deduped.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
