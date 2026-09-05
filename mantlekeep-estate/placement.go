package estate

import (
	"fmt"
	"sort"
	"strings"
)

// Residency is where data is allowed to live. A jurisdiction, not a preference.
//
// It is the one constraint here whose breach cannot be undone: a quota breach is noisy, a wrong
// deployment is redeployable, and personal data landing in a jurisdiction that was never
// approved for it is a reportable incident that no later action reverses. So it is checked FIRST, and capacity pressure can
// never override it.
type Residency string

// Cluster is one deployment target as the platform knows it.
//
// A team never names one. It cannot know that a cluster is full, and naming a cluster makes a
// team responsible for capacity it cannot see — so the platform holds this and decides.
type Cluster struct {
	Name      string    `json:"name"`
	Provider  string    `json:"provider"` // a managed offering, or an on-premises cluster
	Region    string    `json:"region"`   // region identifier, as the operator names it
	Env       string    `json:"env"`      // dev | sit | prod
	Purpose   string    `json:"purpose"`  // app | core | …
	Residency Residency `json:"residency"`
	// Reachable is false when the platform could not read this cluster. An unreachable cluster
	// is UNKNOWN, never empty: placing into one we cannot see would be placing blind.
	Reachable bool `json:"reachable"`
}

// Placement is what a team declares INSTEAD of a cluster name: the requirements its app has.
//
// This is the claim; the chosen cluster is the composite. A team says what it needs, and the
// platform is accountable for the rest.
type Placement struct {
	Env       string    `json:"env"`
	Purpose   string    `json:"purpose,omitempty"`
	Residency Residency `json:"residency,omitempty"`
}

// Capacity is how much room a cluster has, as reported by the metrics source.
//
// REPORTED, not verified — kube-state-metrics tells us this, and it can be stale or wrong. It
// decides between candidates that residency has already approved; it never admits one that
// residency refused.
type Capacity struct {
	Cluster        string  `json:"cluster"`
	AllocatablePct float64 `json:"allocatablePct"` // 0..1, free share of allocatable
}

// Placer chooses a cluster for a claim.
type Placer struct {
	clusters []Cluster
	// capacity is optional. Without it placement still works — it just cannot prefer the
	// emptier of two equally legal clusters, which is a worse answer, not a wrong one.
	capacity map[string]float64
	// minFree is the share of allocatable a cluster must have to be considered. A cluster at
	// its limit is not a candidate: placing there produces pods that will never schedule, and
	// a Deployment that exists but cannot run is the worst failure shape — it looks deployed.
	minFree float64
}

// NewPlacer builds a placer over the registry.
func NewPlacer(clusters []Cluster) *Placer {
	return &Placer{clusters: clusters, capacity: map[string]float64{}, minFree: 0.10}
}

// WithCapacity supplies reported free capacity per cluster.
func (p *Placer) WithCapacity(reports []Capacity) *Placer {
	for _, report := range reports {
		p.capacity[report.Cluster] = report.AllocatablePct
	}
	return p
}

// PlacementDecision records WHERE something was placed and WHY.
//
// Where an app runs is now a platform choice rather than something the team wrote down, so it
// has to be recorded — otherwise nobody can answer "why is my app on that cluster?", and drift
// detection would not know where to look.
type PlacementDecision struct {
	Cluster string `json:"cluster"`
	// Env is the environment of the cluster actually chosen. Recorded because the consequence
	// floor is applied against an environment, and a decision that cannot say which environment
	// it landed in cannot be checked against the tier it was governed at.
	Env string `json:"env,omitempty"`
	// Reason is the sentence a human reads when they ask why.
	Reason string `json:"reason"`
	// Considered lists every cluster that satisfied residency, so a decision can be reviewed
	// against the alternatives rather than taken on trust.
	Considered []string `json:"considered"`
	// Sticky is true when this simply confirmed where the app already runs.
	Sticky bool `json:"sticky"`
}

// Place chooses a cluster for a claim, keeping an existing placement if it is still legal.
//
// `current` is where the app runs today, empty on first placement. Placement is STICKY: an app
// stays where it is unless that becomes illegal. A placer that re-optimised every reconcile
// pass would migrate live apps whenever capacity shifted — silently, unapproved, and
// catastrophically for anything holding state. Moving a placed app is a NEW governed decision,
// never a reconcile outcome.
func (p *Placer) Place(claim Placement, current string) (PlacementDecision, error) {
	if claim.Env == "" {
		return PlacementDecision{}, fmt.Errorf("placement: a claim must name an environment")
	}

	// RESIDENCY FIRST, and alone. Capacity is not consulted until this has filtered, so no
	// amount of pressure elsewhere can push data into the wrong jurisdiction.
	var legal []Cluster
	for _, cluster := range p.clusters {
		if p.legalFor(claim, cluster) {
			legal = append(legal, cluster)
		}
	}
	if len(legal) == 0 {
		return PlacementDecision{}, fmt.Errorf(
			"placement: no cluster satisfies env=%q purpose=%q residency=%q — refusing rather "+
				"than placing somewhere that does not", claim.Env, claim.Purpose, claim.Residency)
	}

	considered := make([]string, 0, len(legal))
	for _, cluster := range legal {
		considered = append(considered, cluster.Name)
	}
	sort.Strings(considered)

	// Sticky: if the app already runs somewhere still legal, it stays. Capacity does not move
	// a running app; only a human does.
	if current != "" {
		for _, cluster := range legal {
			if cluster.Name == current {
				return PlacementDecision{Cluster: current, Env: cluster.Env,
					Considered: considered, Sticky: true,
					Reason: "already placed here and still permitted"}, nil
			}
		}
	}

	// Among legal candidates, prefer the one with the most reported room. A cluster below
	// minFree is skipped: pods that can never schedule look deployed and are not.
	best, bestFree, found := Cluster{}, -1.0, false
	for _, cluster := range legal {
		free, known := p.capacity[cluster.Name]
		if known && free < p.minFree {
			continue
		}
		if !known {
			free = 0 // unknown capacity ranks last, but is still a candidate
		}
		if free > bestFree {
			best, bestFree, found = cluster, free, true
		}
	}
	if !found {
		return PlacementDecision{}, fmt.Errorf(
			"placement: every cluster satisfying env=%q residency=%q is below %.0f%% free — "+
				"placing there would create pods that never schedule",
			claim.Env, claim.Residency, p.minFree*100)
	}

	reason := "the emptiest permitted cluster"
	if _, known := p.capacity[best.Name]; !known {
		reason = "permitted; no capacity reported, so chosen by name"
	}
	if current != "" {
		reason = "moved: " + reason + " (the previous cluster is no longer permitted)"
	}
	return PlacementDecision{Cluster: best.Name, Env: best.Env, Reason: reason,
		Considered: considered}, nil
}

// legalFor reports whether a cluster satisfies the claim. Residency is the hard one.
func (p *Placer) legalFor(claim Placement, cluster Cluster) bool {
	if !strings.EqualFold(cluster.Env, claim.Env) {
		return false
	}
	if claim.Purpose != "" && !strings.EqualFold(cluster.Purpose, claim.Purpose) {
		return false
	}
	// An unspecified residency does NOT mean "anywhere". A claim that says nothing about where
	// its data may live is a claim nobody has ruled on, and defaulting to permitted is how
	// personal data ends up in the wrong jurisdiction by omission.
	if claim.Residency == "" {
		return false
	}
	if !strings.EqualFold(string(cluster.Residency), string(claim.Residency)) {
		return false
	}
	// Never place into a cluster we cannot see. Placing blind produces a record that says
	// deployed and a reality nobody has checked.
	return cluster.Reachable
}
