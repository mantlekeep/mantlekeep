package estate

import "time"

// Floor is what a team may consume, chosen by tier. It lives in SERVER config and is
// unreachable from a manifest.
//
// The default is the point: every grant is floored whether anyone asked or not. A playground
// topic gets a quota and a seven-day retention because it was created, not because someone
// remembered to ask. That single inversion — limits by default rather than on request — is
// what removes the accidental-DoS class, where a team onboards, nobody assigns quotas because
// no step in the process says to, and six months later one bad deploy saturates the cluster.
type Floor struct {
	Kafka    map[Tier]KafkaLimits           `json:"kafka"`
	Postgres map[Tier]PostgresLimits        `json:"postgres"`
	Harbor   map[Tier]HarborLimits          `json:"harbor"`
	App      map[Runtime]map[Tier]AppLimits `json:"app"`
	// Fleet bounds the RUNTIME itself — node pool size, machine shapes, Kubernetes versions.
	// It lives on the same Floor rather than beside it because a deployment configures ONE
	// floor: a second floor object is a second thing to remember to set, and the one nobody
	// sets is the one that is missing when it matters.
	Fleet FleetFloor `json:"fleet"`
	// EnvTiers is the MINIMUM consequence class each environment carries, whatever a manifest
	// declares.
	//
	// Without it the floor is reachable from the request: tier is declared by the team and picks
	// BOTH the gate and the limits, so a manifest saying tier "dev" with placement.env "prod"
	// resolved to no gate and dev-sized limits inside a production cluster. Config chooses the
	// policy; it must never reach the guarantee — and a tier the caller supplies is the caller
	// choosing their own guarantee.
	//
	// A tier is RAISED to the environment's minimum, never lowered: an app may be governed more
	// strictly than its env demands, never less.
	EnvTiers map[string]Tier `json:"envTiers"`
	// Gates is how much human attention each tier costs. In config because a deployment
	// discovers its own answer: a shared DEV cluster that ten teams rely on is not the
	// playground the built-in default assumes, and having to cut a release to say so is the
	// inflexibility that makes teams work around a platform.
	//
	// Config may only make a tier STRONGER than the built-in ([DefaultGates]); the ladder that
	// says which gate outranks which is code. That is the seam: config chooses the policy, it
	// cannot reach the guarantee.
	Gates map[Tier]Gate `json:"gates"`
	// Revision identifies WHICH floor decided, and is derived from the config content — never
	// declared. Stamped onto every intent, so a grant recorded a year ago can still be read
	// against the rules that were actually in force. Without it a hot-reloaded floor makes
	// every past decision irreproducible: an approval that today's limits would refuse is
	// indistinguishable from an error.
	Revision string `json:"-"`
}

// KafkaLimits bound throughput and how long data survives.
type KafkaLimits struct {
	ProducerBytesPerSec int64         `json:"producerBytesPerSec"`
	ConsumerBytesPerSec int64         `json:"consumerBytesPerSec"`
	Retention           time.Duration `json:"retention"`
}

// PostgresLimits bound connections and runaway queries.
//
// Postgres has no byte-rate quota, so connections and timeouts ARE the lever: it forks a
// process per connection, so a pool that nobody bounded is how one app exhausts the instance
// for every other app on it.
type PostgresLimits struct {
	ConnectionLimit          int           `json:"connectionLimit"`
	StatementTimeout         time.Duration `json:"statementTimeout"`
	IdleInTransactionTimeout time.Duration `json:"idleInTransactionTimeout"`
}

// HarborLimits bound how long a non-human credential lives.
type HarborLimits struct {
	RobotExpiry time.Duration `json:"robotExpiry"`
}

// AppLimits bound what one deployment may take from a shared cluster.
type AppLimits struct {
	Replicas  int    `json:"replicas"`
	CPULimit  string `json:"cpuLimit"`
	MemoryMiB int    `json:"memoryMiB"`
}

// DefaultFloor is a floor that scales with harm rather than one global number.
//
// One threshold is wrong in both directions at once: a playground idling three days is noise,
// and a production topic on the same allowance is an incident waiting. These are STARTING
// values a deployment overrides in config; what a deployment cannot do is remove the floor.
func DefaultFloor() Floor {
	return Floor{
		Kafka: map[Tier]KafkaLimits{
			TierDev:    {ProducerBytesPerSec: 10 << 20, ConsumerBytesPerSec: 10 << 20, Retention: 7 * 24 * time.Hour},
			TierShared: {ProducerBytesPerSec: 50 << 20, ConsumerBytesPerSec: 50 << 20, Retention: 30 * 24 * time.Hour},
			TierProd:   {ProducerBytesPerSec: 100 << 20, ConsumerBytesPerSec: 100 << 20, Retention: 30 * 24 * time.Hour},
		},
		Postgres: map[Tier]PostgresLimits{
			TierDev:    {ConnectionLimit: 20, StatementTimeout: 30 * time.Second, IdleInTransactionTimeout: time.Minute},
			TierShared: {ConnectionLimit: 50, StatementTimeout: 2 * time.Minute, IdleInTransactionTimeout: 5 * time.Minute},
			TierProd:   {ConnectionLimit: 100, StatementTimeout: 2 * time.Minute, IdleInTransactionTimeout: 5 * time.Minute},
		},
		Harbor: map[Tier]HarborLimits{
			TierDev:    {RobotExpiry: 90 * 24 * time.Hour},
			TierShared: {RobotExpiry: 90 * 24 * time.Hour},
			TierProd:   {RobotExpiry: 90 * 24 * time.Hour},
		},
		// Per RUNTIME as well as per tier. An analytics app holds its working set in memory
		// and serves a handful of users; an enterprise service is small per replica and
		// scales out. One shared table would starve the first and over-provision the second.
		App: map[Runtime]map[Tier]AppLimits{
			RuntimeEnterprise: {
				TierDev:    {Replicas: 1, CPULimit: "500m", MemoryMiB: 512},
				TierShared: {Replicas: 2, CPULimit: "1", MemoryMiB: 1024},
				TierProd:   {Replicas: 3, CPULimit: "2", MemoryMiB: 2048},
			},
			RuntimeAnalytics: {
				TierDev:    {Replicas: 1, CPULimit: "1", MemoryMiB: 2048},
				TierShared: {Replicas: 1, CPULimit: "2", MemoryMiB: 4096},
				TierProd:   {Replicas: 2, CPULimit: "2", MemoryMiB: 8192},
			},
		},
		Fleet: DefaultFleetFloor(),
		Gates: DefaultGates(),
	}
}

// Gate is the human attention a change costs.
type Gate string

const (
	// GateNone is instant. No consequence means no gate — gating a playground is the ceremony
	// that makes a golden path slower than the bypass, and a bypassed guardrail governs nothing.
	GateNone Gate = "none"
	// GateOwningTeam is approval by the team that owns the affected namespace.
	GateOwningTeam Gate = "owning-team"
	// GatePlatform is approval by the platform, with separation of duties: the approver may
	// not be the requester. Enforced by the door, not by this package.
	GatePlatform Gate = "platform"
)

// DefaultGates is the built-in mapping from consequence to human attention, and the FLOOR
// beneath any configured one: a deployment may raise a tier's gate, never lower it.
func DefaultGates() map[Tier]Gate {
	return map[Tier]Gate{
		TierDev:    GateNone,
		TierShared: GateOwningTeam,
		TierProd:   GatePlatform,
	}
}

// DefaultEnvTiers is the built-in minimum consequence per environment, and the floor beneath any
// configured one: a deployment may raise an environment's minimum, never lower it.
func DefaultEnvTiers() map[string]Tier {
	return map[string]Tier{
		"dev":  TierDev,
		"sit":  TierShared,
		"prod": TierProd,
	}
}

// Strength ranks the tiers so "the environment may only raise" is a comparison rather than a
// list of special cases. It lives in CODE for the same reason [Gate.Strength] does: config that
// could declare its own ranking could call TierDev the strictest and un-govern production.
//
// Zero means unknown, which never satisfies a comparison.
func (t Tier) Strength() int {
	switch t {
	case TierDev:
		return 1
	case TierShared:
		return 2
	case TierProd:
		return 3
	default:
		return 0
	}
}

// MinTierFor reports the least consequence an environment may be governed at, and whether the
// environment is one this floor has ruled on at all.
//
// An unruled environment is NOT permissive by default — the caller refuses rather than guessing,
// because an env nobody configured is a gap in the floor, not a licence.
func (f Floor) MinTierFor(env string) (Tier, bool) {
	if tier, ok := f.EnvTiers[env]; ok {
		return tier, true
	}
	tier, ok := DefaultEnvTiers()[env]
	return tier, ok
}

// AtLeast returns whichever tier costs more attention. The environment can only ever raise.
func AtLeast(declared, minimum Tier) Tier {
	if minimum.Strength() > declared.Strength() {
		return minimum
	}
	return declared
}

// Strength ranks the gates so "config may only tighten" is a comparison rather than a list of
// special cases. It lives in CODE: a config that could declare its own ranking could declare
// GateNone the strongest and un-gate production while every validation still passed.
//
// Zero means unknown, which never satisfies a comparison — an unrecognised gate is refused
// rather than treated as permissive.
func (g Gate) Strength() int {
	switch g {
	case GateNone:
		return 1
	case GateOwningTeam:
		return 2
	case GatePlatform:
		return 3
	default:
		return 0
	}
}

// GateFor maps consequence to human attention. The ONLY place tier becomes a gate, so the
// answer cannot differ between assets — a prod Kafka topic and a prod database cost the same
// attention because the blast radius, not the technology, is what is being governed.
//
// A floor with no configured gates uses the built-in defaults, so a Floor built in a test or
// by [DefaultFloor] behaves exactly as it did when this was a switch.
func (f Floor) GateFor(tier Tier) Gate {
	if gate, ok := f.Gates[tier]; ok {
		return gate
	}
	return DefaultGates()[tier]
}
