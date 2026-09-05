package config

import (
	"encoding/json"
	"fmt"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// duration decodes "168h" rather than 604800000000000.
//
// The wire format the API emits is unaffected — [estate.Floor] still marshals nanoseconds, which
// is what the estate view reads. This type exists only at the authoring edge, the same division
// the manifest makes when it is authored in YAML and parsed as JSON.
type duration time.Duration

func (d *duration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf(
			"a duration must be a quoted string such as \"168h\" or \"30s\", not %s — a bare "+
				"number would be read as nanoseconds, where a dropped zero silently shortens the "+
				"limit", data)
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("%q is not a duration: %w", text, err)
	}
	*d = duration(parsed)
	return nil
}

// floorDocument mirrors [estate.Floor] field for field, differing only in how durations are
// written. Every field is repeated here on purpose: the alternative is decoding straight into
// estate.Floor, which would force the authoring format to be nanoseconds.
type floorDocument struct {
	Kafka    map[estate.Tier]kafkaLimits                  `json:"kafka"`
	Postgres map[estate.Tier]postgresLimits               `json:"postgres"`
	Harbor   map[estate.Tier]harborLimits                 `json:"harbor"`
	App      map[estate.Runtime]map[estate.Tier]appLimits `json:"app"`
	Fleet    fleetFloorDocument                           `json:"fleet"`
	// Gates is optional: absent means the built-in ladder. Present, it may only RAISE a tier's
	// gate — [validateGates] refuses a weaker one, so a config cannot un-gate production.
	//
	// There is deliberately no "revision" field. The revision is derived from this document's
	// bytes, because a declared one can be forgotten on an edit, and a floor claiming a
	// revision it is not is worse than no revision at all.
	Gates map[estate.Tier]estate.Gate `json:"gates,omitempty"`
	// EnvTiers is optional: absent means the built-in minimums. Present, it may only RAISE an
	// environment's minimum consequence — [validateEnvTiers] refuses a weaker one.
	EnvTiers map[string]estate.Tier `json:"envTiers,omitempty"`
}

type kafkaLimits struct {
	ProducerBytesPerSec int64    `json:"producerBytesPerSec"`
	ConsumerBytesPerSec int64    `json:"consumerBytesPerSec"`
	Retention           duration `json:"retention"`
}

type postgresLimits struct {
	ConnectionLimit          int      `json:"connectionLimit"`
	StatementTimeout         duration `json:"statementTimeout"`
	IdleInTransactionTimeout duration `json:"idleInTransactionTimeout"`
}

type harborLimits struct {
	RobotExpiry duration `json:"robotExpiry"`
}

type appLimits struct {
	Replicas  int    `json:"replicas"`
	CPULimit  string `json:"cpuLimit"`
	MemoryMiB int    `json:"memoryMiB"`
}

type fleetFloorDocument struct {
	NodePool           map[estate.Tier]nodePoolLimits `json:"nodePool"`
	KubernetesVersions []string                       `json:"kubernetesVersions"`
}

type nodePoolLimits struct {
	MinNodes             int      `json:"minNodes"`
	MaxNodes             int      `json:"maxNodes"`
	AllowedInstanceTypes []string `json:"allowedInstanceTypes"`
}

// toFloor converts the authored document into the floor the engine applies.
func (f floorDocument) toFloor() estate.Floor {
	floor := estate.Floor{
		Kafka:    make(map[estate.Tier]estate.KafkaLimits, len(f.Kafka)),
		Postgres: make(map[estate.Tier]estate.PostgresLimits, len(f.Postgres)),
		Harbor:   make(map[estate.Tier]estate.HarborLimits, len(f.Harbor)),
		App:      make(map[estate.Runtime]map[estate.Tier]estate.AppLimits, len(f.App)),
		Gates:    estate.DefaultGates(),
	}
	// Merged onto the defaults rather than replacing them, exactly as ownership is: a config
	// that names one tier must not silently drop the gates on the two it did not mention.
	for tier, gate := range f.Gates {
		floor.Gates[tier] = gate
	}
	// Merged onto the defaults, not replacing them: naming one environment must not un-rule the
	// others, and an unruled environment is refused at resolve.
	floor.EnvTiers = estate.DefaultEnvTiers()
	for env, tier := range f.EnvTiers {
		floor.EnvTiers[env] = tier
	}
	for tier, limits := range f.Kafka {
		floor.Kafka[tier] = estate.KafkaLimits{
			ProducerBytesPerSec: limits.ProducerBytesPerSec,
			ConsumerBytesPerSec: limits.ConsumerBytesPerSec,
			Retention:           time.Duration(limits.Retention),
		}
	}
	for tier, limits := range f.Postgres {
		floor.Postgres[tier] = estate.PostgresLimits{
			ConnectionLimit:          limits.ConnectionLimit,
			StatementTimeout:         time.Duration(limits.StatementTimeout),
			IdleInTransactionTimeout: time.Duration(limits.IdleInTransactionTimeout),
		}
	}
	for tier, limits := range f.Harbor {
		floor.Harbor[tier] = estate.HarborLimits{RobotExpiry: time.Duration(limits.RobotExpiry)}
	}
	for runtime, byTier := range f.App {
		converted := make(map[estate.Tier]estate.AppLimits, len(byTier))
		for tier, limits := range byTier {
			converted[tier] = estate.AppLimits{
				Replicas: limits.Replicas, CPULimit: limits.CPULimit, MemoryMiB: limits.MemoryMiB,
			}
		}
		floor.App[runtime] = converted
	}
	floor.Fleet = estate.FleetFloor{
		NodePool:           make(map[estate.Tier]estate.NodePoolLimits, len(f.Fleet.NodePool)),
		KubernetesVersions: f.Fleet.KubernetesVersions,
	}
	for tier, limits := range f.Fleet.NodePool {
		floor.Fleet.NodePool[tier] = estate.NodePoolLimits{
			MinNodes:             limits.MinNodes,
			MaxNodes:             limits.MaxNodes,
			AllowedInstanceTypes: limits.AllowedInstanceTypes,
		}
	}
	return floor
}
