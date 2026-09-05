package estate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Tier is the consequence class of a change: it chooses BOTH the gate and the limits.
//
// A team picks its blast radius, and the floor follows. That coupling is deliberate — letting
// a team choose a tier and then argue separately about its quota would put the limit back
// within reach of the request.
type Tier string

const (
	// TierDev is a playground: no gate, provisioned immediately, and still floored.
	TierDev Tier = "dev"
	// TierShared affects other teams, so the owning team approves.
	TierShared Tier = "shared"
	// TierProd is irreversible or customer-facing: the platform approves, and the approver
	// may not be the requester.
	TierProd Tier = "prod"
)

func (t Tier) valid() bool {
	return t == TierDev || t == TierShared || t == TierProd
}

// rank orders tiers by blast radius so a per-item tier can be checked for LOWERING.
func (t Tier) rank() int {
	switch t {
	case TierShared:
		return 1
	case TierProd:
		return 2
	default:
		return 0
	}
}

// name is the shape every team and resource name must take. Deliberately narrow: these become
// Kafka principals, Postgres identifiers and Harbor project names, and a name that is legal in
// one and not another turns into a provisioning failure halfway through a fan-out.
var name = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

// Manifest is a team's declared footprint across the footprint.
//
// There is no quota, retention, connection-limit or expiry field anywhere in this type, and
// [ParseManifest] rejects unknown fields. That absence IS the sealed floor: a manifest that
// tries to name a limit fails to parse.
type Manifest struct {
	Team           string         `json:"team"`
	Owns           string         `json:"owns"`
	Tier           Tier           `json:"tier,omitempty"`
	Classification string         `json:"classification,omitempty"`
	Apps           []App          `json:"apps,omitempty"`
	Kafka          *KafkaSection  `json:"kafka,omitempty"`
	Postgres       []PostgresBind `json:"postgres,omitempty"`
	Harbor         *HarborSection `json:"harbor,omitempty"`
}

// App is one deployable, and the reason this manifest exists: "deploy all apps from just some
// config about apps".
//
// The IMAGE is a repository reference, never a tag like :latest. A tag is a moving pointer, so
// approving one approves whatever it points at tomorrow — which is the gap between "they
// approved deploy v7" and "v8 shipped". The DIGEST is resolved at approval time and travels on
// the chain, so the control plane deploys what was approved or nothing.
//
// Replicas and resources are deliberately absent: they are floor concerns, chosen by tier. An
// app that could name its own replica count could exhaust a cluster it shares.
type App struct {
	Name string `json:"name"`
	// Runtime is what the platform must know to deploy this: a long-running service and an
	// in-memory analytics app need different base images, ports and resource shapes. Declared rather
	// than inferred from the image — inference fails SILENTLY the day someone's repository
	// does not match the naming convention, and a silent wrong default deploys the wrong thing.
	Runtime Runtime `json:"runtime"`
	// Image is the repository, WITHOUT a tag or digest. The digest is bound at approval.
	Image string `json:"image"`
	// Placement is WHAT THIS APP NEEDS, not where it goes. A team cannot know that a cluster
	// is full, and naming one makes it responsible for capacity it cannot see — so it declares
	// env, purpose and residency, and the platform chooses. Required: an app with no placement
	// has no jurisdiction, and defaulting one would let data land somewhere nobody ruled on.
	Placement Placement `json:"placement"`
	// Tier may raise the consequence for this one app — a customer-facing app in an otherwise
	// dev-tier team.
	Tier Tier `json:"tier,omitempty"`
}

// Runtime is the app shape the platform serves — enterprise, analytics, and whatever a deployment
// adds next.
//
// WHICH runtimes exist is CONFIG, not a constant list. The floor already enumerates them (it
// carries limits per runtime), so the floor is the single source of truth and [Resolve]
// refuses a runtime it has no floor for. A hardcoded list here would be a SECOND place to edit,
// and the two would disagree the first time somebody updated only one — the manifest accepting
// a runtime no adapter can deploy.
//
// The named constants below are the two shipped today, kept so callers and tests can refer to
// them without stringly-typed literals. They are not a closed set.
type Runtime string

const (
	// RuntimeEnterprise is a long-running service.
	RuntimeEnterprise Runtime = "enterprise"
	// RuntimeAnalytics is an analytical app that holds its working set in memory.
	RuntimeAnalytics Runtime = "analytics"
)

// wellFormed checks the SHAPE of a runtime name. Whether it EXISTS is the floor's answer.
func (r Runtime) wellFormed() bool {
	return name.MatchString(string(r))
}

// KafkaSection is what a team wants from Kafka. `ConsumesFrom` names OTHER teams' namespaces:
// a cross-team read is always an explicit entry, never a side effect of a default left open.
type KafkaSection struct {
	Cluster      string   `json:"cluster,omitempty"`
	Topics       []Item   `json:"topics,omitempty"`
	ConsumesFrom []string `json:"consumesFrom,omitempty"`
}

// PostgresBind is ONE binding. Bindings are a list because the real cases are messy: two apps
// sharing a database with separate schemas, and one app needing a second cluster for
// analytical load. Both fall out of a list; neither needs a special case.
type PostgresBind struct {
	Cluster  string   `json:"cluster"`
	Database string   `json:"database"`
	Schema   string   `json:"schema,omitempty"`
	Readers  []string `json:"readers,omitempty"`
	Tier     Tier     `json:"tier,omitempty"`
}

// HarborSection carries CI robots. They are named here so their creation is governed and their
// expiry comes from the floor — a robot account is a long-lived credential with registry write
// access, which is the supply-chain position in an air-gapped footprint.
type HarborSection struct {
	Project string `json:"project,omitempty"`
	Robots  []Item `json:"robots,omitempty"`
	Tier    Tier   `json:"tier,omitempty"`
}

// Item is a resource name, optionally raising its own tier.
//
// It decodes from either a bare string ("orders") or an object ({"name":"settlements",
// "tier":"prod"}). That union is the entire extend/override mechanism: a team is mostly dev
// with one production topic, and nobody has to choose between "everything is a playground"
// and "everything needs approval".
type Item struct {
	Name string `json:"name"`
	Tier Tier   `json:"tier,omitempty"`
}

// UnmarshalJSON accepts the bare-string form as well as the object form.
func (i *Item) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var bare string
		if err := json.Unmarshal(data, &bare); err != nil {
			return err
		}
		i.Name = bare
		i.Tier = ""
		return nil
	}
	// A named type, so the object form does not recurse back into this method.
	type item struct {
		Name string `json:"name"`
		Tier Tier   `json:"tier,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var object item
	if err := decoder.Decode(&object); err != nil {
		return err
	}
	i.Name, i.Tier = object.Name, object.Tier
	return nil
}

// ParseManifest decodes and validates a manifest.
//
// Unknown fields are REFUSED rather than ignored. That is the whole point: silently dropping
// an unrecognised field is how a team believes it set a limit that was never applied, and how
// a deployment believes it has a rule it does not have.
func ParseManifest(document []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()

	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	if manifest.Tier == "" {
		manifest.Tier = TierDev
	}
	if err := manifest.validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) validate() error {
	if !name.MatchString(m.Team) {
		return fmt.Errorf("manifest: team %q is not a valid name", m.Team)
	}
	if !name.MatchString(m.Owns) {
		return fmt.Errorf("manifest: owns %q is not a valid name", m.Owns)
	}
	if !m.Tier.valid() {
		return fmt.Errorf("manifest: tier %q is not one of dev, shared, prod", m.Tier)
	}
	for _, app := range m.Apps {
		if !name.MatchString(app.Name) {
			return fmt.Errorf("manifest: app %q is not a valid name", app.Name)
		}
		if !app.Runtime.wellFormed() {
			return fmt.Errorf(
				"manifest: app %q has runtime %q, which is not a valid runtime name — the "+
					"runtime decides how the platform deploys it, so there is no safe default",
				app.Name, app.Runtime)
		}
		if app.Image == "" {
			return fmt.Errorf("manifest: app %q needs an image repository", app.Name)
		}
		// A tag or digest here would let a team pin what gets deployed, bypassing the digest
		// the approval binds to. The reference is resolved at approval, not declared.
		if strings.ContainsAny(app.Image, ":@") {
			return fmt.Errorf(
				"manifest: app %q image %q must not carry a tag or digest — the digest is "+
					"resolved at approval and recorded, so what deploys is what was approved",
				app.Name, app.Image)
		}
		if app.Tier != "" && !app.Tier.valid() {
			return fmt.Errorf("manifest: app %q has tier %q, not one of dev, shared, prod",
				app.Name, app.Tier)
		}
		if app.Placement.Env == "" {
			return fmt.Errorf(
				"manifest: app %q has no placement — it must declare at least an environment, "+
					"and a residency, so the platform can choose a cluster that is permitted",
				app.Name)
		}
		if app.Placement.Residency == "" {
			// Silence must not become permission. This is the one refusal whose absence is
			// unrecoverable: data placed in the wrong jurisdiction cannot be un-placed.
			return fmt.Errorf(
				"manifest: app %q declares no residency — a claim nobody has ruled on must not "+
					"become permission by omission", app.Name)
		}
		if app.Tier != "" && app.Tier.rank() < m.Tier.rank() {
			return fmt.Errorf(
				"manifest: app %q lowers the tier to %q below the manifest's %q — an item may "+
					"RAISE its consequence, never lower it", app.Name, app.Tier, m.Tier)
		}
	}
	if m.Kafka != nil {
		for _, topic := range m.Kafka.Topics {
			if err := m.checkItem("kafka topic", topic); err != nil {
				return err
			}
		}
	}
	if m.Harbor != nil {
		for _, robot := range m.Harbor.Robots {
			if err := m.checkItem("harbor robot", robot); err != nil {
				return err
			}
		}
	}
	for _, bind := range m.Postgres {
		if bind.Cluster == "" || bind.Database == "" {
			return fmt.Errorf("manifest: a postgres binding needs both a cluster and a database")
		}
		if bind.Tier != "" && !bind.Tier.valid() {
			return fmt.Errorf("manifest: postgres tier %q is not one of dev, shared, prod", bind.Tier)
		}
		if bind.Tier != "" && bind.Tier.rank() < m.Tier.rank() {
			return fmt.Errorf(
				"manifest: postgres binding %s/%s lowers the tier to %q below the manifest's %q — "+
					"an item may RAISE its consequence, never lower it",
				bind.Cluster, bind.Database, bind.Tier, m.Tier)
		}
	}
	return nil
}

func (m Manifest) checkItem(what string, item Item) error {
	if !name.MatchString(item.Name) {
		return fmt.Errorf("manifest: %s %q is not a valid name", what, item.Name)
	}
	if item.Tier != "" && !item.Tier.valid() {
		return fmt.Errorf("manifest: %s %q has tier %q, not one of dev, shared, prod",
			what, item.Name, item.Tier)
	}
	// Raising is the point; lowering would let a team declare a production topic to be a
	// playground and take its gate away.
	if item.Tier != "" && item.Tier.rank() < m.Tier.rank() {
		return fmt.Errorf(
			"manifest: %s %q lowers the tier to %q below the manifest's %q — an item may RAISE "+
				"its consequence, never lower it",
			what, item.Name, item.Tier, m.Tier)
	}
	return nil
}

// tierOf returns the effective tier for an item: its own if it raised one, else the manifest's.
func (m Manifest) tierOf(item Item) Tier {
	if item.Tier != "" {
		return item.Tier
	}
	return m.Tier
}
