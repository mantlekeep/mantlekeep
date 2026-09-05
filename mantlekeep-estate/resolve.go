package estate

import "fmt"

// Desired is a manifest with the floor applied: what the adapters receive, what an approval
// binds to, and what a reconciler compares reality against.
//
// Nobody authors this. It is computed, which is what makes it safe to compare — a desired
// state a team could edit directly would be a desired state that could drift from what was
// approved.
type Desired struct {
	Team    string        `json:"team"`
	Owns    string        `json:"owns"`
	Changes []DesiredItem `json:"changes"`
}

// DesiredItem is one resource, floored and gated. Flat on purpose: a reconciler diffs a list,
// and a nested tree would need a walker that has to agree with the differ.
type DesiredItem struct {
	Asset   string `json:"asset"` // kafka | postgres | harbor | app
	Kind    string `json:"kind"`  // boundary | topic | schema | project | robot | deployment
	Name    string `json:"name"`  // fully qualified, already prefixed with the namespace
	Tier    Tier   `json:"tier"`
	Gate    Gate   `json:"gate"`
	Cluster string `json:"cluster,omitempty"`
	// Slot is WHERE this runs. Two versions of one app in the same cluster, in different
	// namespaces, are two slots — comparing by name alone makes each look like drift from
	// the other, and the reconciler would overwrite a live version to "fix" it.
	Slot    Slot     `json:"slot,omitempty"`
	Limits  any      `json:"limits"` // the floor's values for this tier
	Readers []string `json:"readers,omitempty"`
	Image   string   `json:"image,omitempty"`   // app only: repository, digest bound at approval
	Runtime string   `json:"runtime,omitempty"` // app only: enterprise | analytics
	// Digest is the artifact approved FOR THIS SLOT. Empty until something is promoted here,
	// which is why a slot running an older digest than its neighbour is correct rather than
	// stale: approval is per slot, not per app.
	Digest string `json:"digest,omitempty"`
	// Placement records WHICH cluster was chosen and why, when the platform chose it.
	Placement *PlacementDecision `json:"placement,omitempty"`
	// State is the approved VALUES of this item's own fields, keyed by the same field names
	// [Ownership] uses. An app expresses its state as a digest and an image; a cluster
	// expresses it as a Kubernetes version, a storage class and a node count. A typed field
	// per asset would put asset knowledge in the differ, which is the one place that must stay
	// generic — so the shape is a map and the vocabulary is the ownership map's.
	State map[string]string `json:"state,omitempty"`
}

// Resolve applies the floor to a manifest.
//
// The team's namespace comes FIRST and is granted once: it is the boundary everything else
// lives inside, and the reason a later topic needs no gate of its own.
// Resolve applies the floor to a manifest, choosing a cluster for each app.
//
// Placement needs a placer; ResolveWith supplies one. Resolve keeps the old signature for
// callers with no fleet — it refuses any manifest containing apps rather than inventing a
// cluster, because a placement nobody decided is exactly what the claim model removes.
func Resolve(m Manifest, floor Floor) (Desired, error) {
	return ResolveWith(m, floor, nil, nil)
}

// ResolveWith resolves a manifest against a fleet.
//
// `placed` maps an app name to where it already runs, so placement stays STICKY across passes:
// without it every reconcile would re-choose, and a capacity shift would migrate a live app
// silently. Pass nil on first placement.
func ResolveWith(m Manifest, floor Floor, placer *Placer, placed map[string]string) (Desired, error) {
	return resolve(m, floor, placer, placed)
}

// resolve walks each asset section in turn.
//
// One function per section, rather than one long one: the sections share no state beyond the
// manifest and the floor, and reading "what does a Kafka declaration become" should not mean
// scrolling past Postgres to find out. Each returns its own changes so the caller does the
// appending and no helper can quietly append twice.
func resolve(m Manifest, floor Floor, placer *Placer, placed map[string]string) (Desired, error) {
	desired := Desired{Team: m.Team, Owns: m.Owns}

	for _, section := range []func(Manifest, Floor) ([]DesiredItem, error){
		resolveKafka, resolvePostgres, resolveHarbor,
	} {
		changes, err := section(m, floor)
		if err != nil {
			return Desired{}, err
		}
		desired.Changes = append(desired.Changes, changes...)
	}

	apps, err := resolveApps(m, floor, placer, placed)
	if err != nil {
		return Desired{}, err
	}
	desired.Changes = append(desired.Changes, apps...)

	return desired, nil
}

// resolveKafka turns a Kafka declaration into a boundary and its topics.
func resolveKafka(m Manifest, floor Floor) ([]DesiredItem, error) {
	if m.Kafka == nil {
		return nil, nil
	}
	limits, ok := floor.Kafka[m.Tier]
	if !ok {
		return nil, fmt.Errorf("resolve: no kafka floor configured for tier %q", m.Tier)
	}
	// The boundary itself. Its tier is the manifest's, because taking a namespace is a
	// team-level act however trivial the first topic inside it is.
	changes := []DesiredItem{{
		Asset: "kafka", Kind: "boundary", Name: m.Owns + ".", Tier: m.Tier,
		Gate: floor.GateFor(m.Tier), Cluster: m.Kafka.Cluster, Limits: limits,
	}}
	for _, topic := range m.Kafka.Topics {
		tier := m.tierOf(topic)
		topicLimits, ok := floor.Kafka[tier]
		if !ok {
			return nil, fmt.Errorf("resolve: no kafka floor for tier %q", tier)
		}
		changes = append(changes, DesiredItem{
			Asset: "kafka", Kind: "topic", Name: m.Owns + "." + topic.Name, Tier: tier,
			Gate: floor.GateFor(tier), Cluster: m.Kafka.Cluster, Limits: topicLimits,
		})
	}
	return changes, nil
}

// resolvePostgres turns each database binding into a governed schema.
func resolvePostgres(m Manifest, floor Floor) ([]DesiredItem, error) {
	var changes []DesiredItem
	for _, bind := range m.Postgres {
		tier := orTier(bind.Tier, m.Tier)
		limits, ok := floor.Postgres[tier]
		if !ok {
			return nil, fmt.Errorf("resolve: no postgres floor for tier %q", tier)
		}
		schema := orName(bind.Schema, m.Owns)
		changes = append(changes, DesiredItem{
			Asset: "postgres", Kind: "schema",
			Name: bind.Database + "." + schema, Tier: tier, Gate: floor.GateFor(tier),
			Cluster: bind.Cluster, Limits: limits, Readers: bind.Readers,
		})
	}
	return changes, nil
}

// resolveHarbor turns a registry declaration into a project and its robots.
func resolveHarbor(m Manifest, floor Floor) ([]DesiredItem, error) {
	if m.Harbor == nil {
		return nil, nil
	}
	tier := orTier(m.Harbor.Tier, m.Tier)
	limits, ok := floor.Harbor[tier]
	if !ok {
		return nil, fmt.Errorf("resolve: no harbor floor for tier %q", tier)
	}
	project := orName(m.Harbor.Project, m.Owns)
	changes := []DesiredItem{{
		Asset: "harbor", Kind: "project", Name: project, Tier: tier,
		Gate: floor.GateFor(tier), Limits: limits,
	}}
	for _, robot := range m.Harbor.Robots {
		robotTier := m.tierOf(robot)
		robotLimits, ok := floor.Harbor[robotTier]
		if !ok {
			return nil, fmt.Errorf("resolve: no harbor floor for tier %q", robotTier)
		}
		changes = append(changes, DesiredItem{
			Asset: "harbor", Kind: "robot", Name: project + "/" + robot.Name,
			Tier: robotTier, Gate: floor.GateFor(robotTier), Limits: robotLimits,
		})
	}
	return changes, nil
}

// resolveApps turns each app into a placed deployment.
//
// It takes the placer because an app is the only section whose target the PLATFORM chooses
// rather than the team.
func resolveApps(m Manifest, floor Floor, placer *Placer,
	placed map[string]string) ([]DesiredItem, error) {

	var changes []DesiredItem
	for _, app := range m.Apps {
		tier, limits, err := appFloor(m, floor, app)
		if err != nil {
			return nil, err
		}
		decision, err := placeApp(app, placer, placed[app.Name])
		if err != nil {
			return nil, err
		}
		deployment := m.Owns + "-" + app.Name
		changes = append(changes, DesiredItem{
			Asset: "app", Kind: "deployment", Name: deployment, Tier: tier,
			Gate: floor.GateFor(tier), Cluster: decision.Cluster, Limits: limits, Image: app.Image,
			Runtime: string(app.Runtime),
			Slot:    Slot{Cluster: decision.Cluster, Namespace: m.Owns, Name: deployment},
			// Where an app runs is now a PLATFORM choice, so it travels with the change and
			// reaches the chain. Without it nobody can answer "why is my app on that cluster?"
			Placement: &decision,
		})
	}
	return changes, nil
}

// appFloor resolves the tier an app is governed at, and the limits that tier carries.
//
// The ENVIRONMENT raises the tier before anything is looked up under it. Tier is declared by
// the team and chooses both the gate and the limits, so without this a manifest saying tier
// "dev" with placement.env "prod" got GateNone and dev-sized limits inside a production
// cluster — config reaching the guarantee, which is the one thing the floor exists to prevent.
func appFloor(m Manifest, floor Floor, app App) (Tier, any, error) {
	byTier, ok := floor.App[app.Runtime]
	if !ok {
		return "", nil, fmt.Errorf(
			"resolve: runtime %q is not configured — the floor enumerates the runtimes this "+
				"deployment serves, so an unknown one is refused rather than defaulted",
			app.Runtime)
	}
	// An unruled environment is refused rather than guessed: an env nobody configured is a gap
	// in the floor, not a licence.
	minimum, ruled := floor.MinTierFor(app.Placement.Env)
	if !ruled {
		return "", nil, fmt.Errorf(
			"resolve: app %q targets env %q, which the floor has not ruled on — an "+
				"environment with no minimum consequence would be governed at whatever "+
				"tier the request asked for", app.Name, app.Placement.Env)
	}
	tier := AtLeast(orTier(app.Tier, m.Tier), minimum)

	limits, ok := byTier[tier]
	if !ok {
		return "", nil, fmt.Errorf("resolve: no %q app floor for tier %q", app.Runtime, tier)
	}
	return tier, limits, nil
}

// placeApp asks the fleet where this app belongs, and checks the answer honoured the
// environment the tier was raised against.
func placeApp(app App, placer *Placer, sticky string) (PlacementDecision, error) {
	// A slot, even for a manifest-declared app. Without one these compare by name and collide
	// the moment the same app runs in two namespaces — the side-by-side changeover case.
	if placer == nil {
		return PlacementDecision{}, fmt.Errorf(
			"resolve: app %q needs a cluster and no fleet was supplied — use ResolveWith; "+
				"inventing a placement would put data somewhere nobody ruled on", app.Name)
	}
	decision, err := placer.Place(app.Placement, sticky)
	if err != nil {
		return PlacementDecision{}, fmt.Errorf("resolve: app %q: %w", app.Name, err)
	}
	// The tier was raised against the DECLARED env; placement must have honoured it, or the
	// item would be governed for one environment and land in another.
	if decision.Env != "" && decision.Env != app.Placement.Env {
		return PlacementDecision{}, fmt.Errorf(
			"resolve: app %q was governed for env %q but placed on %q (cluster %q) — the "+
				"tier floor was applied to the wrong environment",
			app.Name, app.Placement.Env, decision.Env, decision.Cluster)
	}
	return decision, nil
}

// orTier returns the declared tier when a section sets one, else the manifest's.
func orTier(declared, fallback Tier) Tier {
	if declared != "" {
		return declared
	}
	return fallback
}

// orName returns the declared name when a section sets one, else the team's own.
func orName(declared, fallback string) string {
	if declared != "" {
		return declared
	}
	return fallback
}

// NeedsGate reports whether anything in this desired state costs human attention. A change set
// that is entirely dev tier is applied immediately — which is the common case and must stay
// fast, or teams route around the platform.
func (d Desired) NeedsGate() bool {
	for _, change := range d.Changes {
		if change.Gate != GateNone {
			return true
		}
	}
	return false
}
