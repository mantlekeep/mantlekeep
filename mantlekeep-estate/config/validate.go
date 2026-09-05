package config

import (
	"fmt"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// tiers is every consequence class a floor must answer for. [estate.Resolve] already refuses a
// tier it has no floor for, which is fail-closed but late: the team that discovers it is the
// one whose apply failed. Checking at BOOT turns a per-request surprise into a config error
// the operator sees before anything is serving.
var tiers = []estate.Tier{estate.TierDev, estate.TierShared, estate.TierProd}

// validateFloor refuses a floor that is missing or toothless.
//
// A zero is the danger, not a malformed value. "producerBytesPerSec": 0 parses cleanly and
// reads like a limit, and an engine that applies it grants an unbounded rate under the name of
// a quota. That is config reaching the guarantee, so every limit must be POSITIVE — the one
// exception is a node pool's minimum, where zero is a real choice (a playground may scale to
// nothing) and the ceiling is the limit that matters.
func validateFloor(floor estate.Floor) error {
	for _, tier := range tiers {
		limits, ok := floor.Kafka[tier]
		if !ok {
			return missing("kafka", tier)
		}
		if err := positive("kafka", tier, "producerBytesPerSec", limits.ProducerBytesPerSec); err != nil {
			return err
		}
		if err := positive("kafka", tier, "consumerBytesPerSec", limits.ConsumerBytesPerSec); err != nil {
			return err
		}
		if err := positive("kafka", tier, "retention", int64(limits.Retention)); err != nil {
			return err
		}

		postgres, ok := floor.Postgres[tier]
		if !ok {
			return missing("postgres", tier)
		}
		if err := positive("postgres", tier, "connectionLimit", int64(postgres.ConnectionLimit)); err != nil {
			return err
		}
		if err := positive("postgres", tier, "statementTimeout", int64(postgres.StatementTimeout)); err != nil {
			return err
		}
		if err := positive("postgres", tier, "idleInTransactionTimeout",
			int64(postgres.IdleInTransactionTimeout)); err != nil {
			return err
		}

		harbor, ok := floor.Harbor[tier]
		if !ok {
			return missing("harbor", tier)
		}
		// A robot account is a long-lived credential with registry write access. An expiry of
		// zero is a credential that never expires.
		if err := positive("harbor", tier, "robotExpiry", int64(harbor.RobotExpiry)); err != nil {
			return err
		}
	}

	if len(floor.App) == 0 {
		return fmt.Errorf(
			"config: the floor configures no app runtimes — the floor is the only place runtimes " +
				"are enumerated, so an empty one means no app can ever be resolved")
	}
	for runtime, byTier := range floor.App {
		for _, tier := range tiers {
			limits, ok := byTier[tier]
			if !ok {
				return missing("app runtime "+string(runtime), tier)
			}
			if err := positive(string(runtime), tier, "replicas", int64(limits.Replicas)); err != nil {
				return err
			}
			if err := positive(string(runtime), tier, "memoryMiB", int64(limits.MemoryMiB)); err != nil {
				return err
			}
			if limits.CPULimit == "" {
				return fmt.Errorf(
					"config: floor app.%s.%s.cpuLimit is empty — an app with no CPU ceiling can "+
						"starve every other app on the node", runtime, tier)
			}
		}
	}
	return validateFleet(floor.Fleet)
}

// validateFleet refuses a runtime floor that bounds nothing.
func validateFleet(fleet estate.FleetFloor) error {
	if len(fleet.KubernetesVersions) == 0 {
		return fmt.Errorf(
			"config: floor fleet.kubernetesVersions is empty — an empty set refuses every " +
				"version, so no cluster could be declared at all")
	}
	for _, tier := range tiers {
		limits, ok := fleet.NodePool[tier]
		if !ok {
			return missing("fleet nodePool", tier)
		}
		if err := positive("fleet nodePool", tier, "maxNodes", int64(limits.MaxNodes)); err != nil {
			return err
		}
		// minNodes may legitimately be zero — scale-to-zero is a real choice for a playground —
		// so it is bounded only against the ceiling.
		if limits.MinNodes < 0 || limits.MinNodes > limits.MaxNodes {
			return fmt.Errorf(
				"config: floor fleet.nodePool.%s has minNodes %d against maxNodes %d — a floor "+
					"above its own ceiling can never be satisfied", tier, limits.MinNodes, limits.MaxNodes)
		}
		if len(limits.AllowedInstanceTypes) == 0 {
			return fmt.Errorf(
				"config: floor fleet.nodePool.%s.allowedInstanceTypes is empty — without it "+
					"\"change the instance type\" is unbounded spend under a light gate", tier)
		}
	}
	return nil
}

func missing(asset string, tier estate.Tier) error {
	return fmt.Errorf(
		"config: the floor has no %s limits for tier %q — every tier must be floored, because a "+
			"tier with no entry is a tier with no limits", asset, tier)
}

func positive(asset string, tier estate.Tier, field string, value int64) error {
	if value > 0 {
		return nil
	}
	return fmt.Errorf(
		"config: floor %s.%s.%s is %d — a limit of zero reads like a limit and grants an "+
			"unbounded one, which is config reaching the guarantee rather than choosing the policy",
		asset, tier, field, value)
}

// validateGates refuses a config that would cost LESS human attention than the built-in
// ladder, and refuses a gate nobody recognises.
//
// This is where "config may tighten, never loosen" is actually enforced for gates. Raising
// dev from none to owning-team is exactly the case this exists to allow: a shared DEV cluster
// that ten teams depend on is not the playground the default assumes. Lowering prod from
// platform to none is the case it exists to stop, and it is refused even though every limit
// in the same file may be perfectly sound.
func validateGates(floor estate.Floor) error {
	defaults := estate.DefaultGates()
	for _, tier := range tiers {
		gate, ok := floor.Gates[tier]
		if !ok {
			return fmt.Errorf("config: floor.gates has no gate for tier %q — a tier with no "+
				"gate has no answer for how much attention it costs", tier)
		}
		if gate.Strength() == 0 {
			return fmt.Errorf("config: floor.gates[%q] is %q, which is not a gate this build "+
				"knows — an unrecognised gate is refused rather than assumed permissive", tier, gate)
		}
		if builtin := defaults[tier]; gate.Strength() < builtin.Strength() {
			return fmt.Errorf("config: floor.gates[%q] is %q, weaker than the built-in %q — "+
				"config chooses the policy and may raise a gate, but it cannot lower the floor",
				tier, gate, builtin)
		}
	}
	return nil
}

// validateEnvTiers refuses a floor that would let an environment be governed more loosely than
// the built-in ladder allows.
//
// This is where "the environment may only raise" is actually enforced. Lowering prod's minimum
// to dev would restore exactly the hole this floor exists to close: a manifest declaring
// tier "dev" against a production cluster, resolving to no gate and dev-sized limits.
func validateEnvTiers(floor estate.Floor) error {
	defaults := estate.DefaultEnvTiers()
	for env, tier := range floor.EnvTiers {
		if tier.Strength() == 0 {
			return fmt.Errorf("config: floor.envTiers[%q] is %q, which is not a tier this build "+
				"knows — an unrecognised tier is refused rather than assumed harmless", env, tier)
		}
		if builtin, known := defaults[env]; known && tier.Strength() < builtin.Strength() {
			return fmt.Errorf("config: floor.envTiers[%q] is %q, weaker than the built-in %q — "+
				"config may raise an environment's minimum consequence, never lower it",
				env, tier, builtin)
		}
	}
	return nil
}
