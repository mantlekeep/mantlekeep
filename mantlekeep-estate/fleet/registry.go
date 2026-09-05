// Package fleet loads the cluster registry and reads reported capacity.
//
// Both are INPUTS to placement, and neither may decide anything on its own: the registry says
// which clusters exist and what jurisdiction each serves; capacity ranks the candidates that
// residency has already permitted. Keeping them here, outside the decision layer, is what lets
// a deployment swap a static file for a live registry without touching a placement rule.
package fleet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/internal/safepath"
	"os"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// document is the registry file's shape.
type document struct {
	Clusters []cluster `json:"clusters"`
}

type cluster struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Region    string `json:"region"`
	Env       string `json:"env"`
	Purpose   string `json:"purpose"`
	Residency string `json:"residency"`
}

// Load reads the cluster registry.
//
// Unknown fields are REFUSED, as everywhere else here and for the same reason: a misspelled
// residency that is silently ignored is worse than a missing one, because the operator believes
// the jurisdiction is recorded. On this document that mistake places data in the wrong country.
func Load(path string) ([]estate.Cluster, error) {
	if path == "" {
		return nil, fmt.Errorf(
			"fleet: no registry path — a control plane with no fleet can place nothing, and " +
				"defaulting to an empty one would refuse every app with a confusing message")
	}
	clean, err := safepath.Clean(path)
	if err != nil {
		return nil, fmt.Errorf("fleet: %w", err)
	}
	content, err := os.ReadFile(clean) // #nosec G304 -- operator-supplied path, guarded above
	if err != nil {
		return nil, fmt.Errorf("fleet: %w", err)
	}
	return Parse(content)
}

// Parse validates and converts the registry document.
func Parse(content []byte) ([]estate.Cluster, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()

	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("fleet: %w", err)
	}
	if len(doc.Clusters) == 0 {
		return nil, fmt.Errorf("fleet: the registry names no clusters")
	}

	seen := make(map[string]bool, len(doc.Clusters))
	clusters := make([]estate.Cluster, 0, len(doc.Clusters))
	for _, entry := range doc.Clusters {
		if entry.Name == "" {
			return nil, fmt.Errorf("fleet: a cluster with no name cannot be placed into")
		}
		if seen[entry.Name] {
			// Two entries with one name means one of them is invisible, and which one wins is
			// map-order luck. That is a placement decided by chance.
			return nil, fmt.Errorf("fleet: cluster %q is listed twice", entry.Name)
		}
		seen[entry.Name] = true

		if entry.Env == "" {
			return nil, fmt.Errorf("fleet: cluster %q has no env — nothing could ever match it",
				entry.Name)
		}
		if entry.Residency == "" {
			// The jurisdiction is the one field whose absence is unrecoverable. A cluster that
			// does not say where it is must never be a candidate, and saying so at load time
			// beats discovering it when data has already landed.
			return nil, fmt.Errorf(
				"fleet: cluster %q declares no residency — a cluster whose jurisdiction is "+
					"unknown must not be placed into", entry.Name)
		}

		clusters = append(clusters, estate.Cluster{
			Name:      entry.Name,
			Provider:  entry.Provider,
			Region:    entry.Region,
			Env:       entry.Env,
			Purpose:   entry.Purpose,
			Residency: estate.Residency(entry.Residency),
			// Reachability is MEASURED, never declared. A registry claiming a cluster is up
			// would be a file asserting a fact about the world; the observer decides this.
			Reachable: false,
		})
	}
	return clusters, nil
}

// MarkReachable sets which clusters the platform could actually read this pass.
//
// Separate from Load on purpose: the registry says what SHOULD exist, the observer says what
// answered. Merging them would let a stale file assert that a dead cluster is available.
func MarkReachable(clusters []estate.Cluster, reachable map[string]bool) []estate.Cluster {
	out := make([]estate.Cluster, len(clusters))
	for i, cluster := range clusters {
		cluster.Reachable = reachable[cluster.Name]
		out[i] = cluster
	}
	return out
}

// Prober decides which clusters answered. One implementation per way of reaching a cluster.
type Prober interface {
	Reachable(clusters []estate.Cluster) map[string]bool
}

// assumeReachable treats every cluster in the registry as up.
//
// NAMED, not defaulted. Reachability is meant to be measured, and a silent default would let a
// registry file assert a fact about the world — the exact thing MarkReachable exists to
// prevent. A deployment that has no prober yet must say so out loud, because every placement it
// then makes is trusting a file rather than a cluster.
type assumeReachable struct{}

// AssumeReachable is for development and for a deployment whose prober is not built yet. It
// asserts nothing and should never be the answer in production.
func AssumeReachable() Prober { return assumeReachable{} }

func (assumeReachable) Reachable(clusters []estate.Cluster) map[string]bool {
	up := make(map[string]bool, len(clusters))
	for _, cluster := range clusters {
		up[cluster.Name] = true
	}
	return up
}
