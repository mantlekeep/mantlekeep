package estate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This file is the DECISION layer for changes to the RUNTIME itself: clusters, node pools,
// Kubernetes versions. Deploying an app ONTO a runtime is one kind of change; changing the
// runtime every app is standing on is another, and it is the one that matters more, which is
// why the tiering below is the whole point. This package is not a second control plane; it is
// the door in front of both kinds of change, so a change to the fleet is parsed, floored,
// gated and reconciled by exactly the same primitives as a change to an app.
//
// THE RULE THAT SHAPES ALL OF IT: consequence is a property of the CHANGE, not of the control
// plane. It would be easy to tier every runtime change as irreversible because "it is the
// runtime". That
// over-gates: scaling a pool from 5 to 8 is undone by scaling it back, while a Kubernetes
// upgrade cannot be undone at all — etcd migrations are one-way, so the "rollback" is a new
// cluster and a migration of every workload onto it. Gating both the same way is what makes a
// team route around the platform, and a bypassed guardrail governs nothing.

// Consequence is how far a change can be walked back. It is the fact the gate is derived from,
// and it is CODE here rather than config or manifest: a requester who could declare its own
// change reversible would have declared its own gate away.
type Consequence string

const (
	// ConsequenceReversible is undone in place, in minutes, by doing the opposite. Nothing is
	// lost on the way back.
	ConsequenceReversible Consequence = "reversible"
	// ConsequenceDisruptive is recoverable but expensive: hours, and workloads move. Draining
	// and replacing nodes gets you back, so it is not one-way — but it is not free either.
	ConsequenceDisruptive Consequence = "disruptive"
	// ConsequenceIrreversible cannot be undone by the reverse operation. Either the old state
	// is unreachable (a Kubernetes downgrade is not a thing) or it survives only for new
	// objects (existing volumes keep the storage class they were created with). The recovery
	// is a rebuild plus a migration, which is a project, not a rollback.
	ConsequenceIrreversible Consequence = "irreversible"
)

// FleetOperation is one kind of change to the runtime. Named per OPERATION rather than per
// asset because that is the unit consequence attaches to: two operations on the SAME node pool
// cost different amounts of human attention.
type FleetOperation string

const (
	// OperationRegisterCluster brings a new cluster under governance.
	OperationRegisterCluster FleetOperation = "register-cluster"
	// OperationRemoveCluster takes one out of the fleet.
	OperationRemoveCluster FleetOperation = "remove-cluster"
	// OperationScaleNodePool changes how many nodes a pool has.
	OperationScaleNodePool FleetOperation = "scale-node-pool"
	// OperationChangeNodeInstanceType replaces the machine shape a pool's nodes are built from.
	OperationChangeNodeInstanceType FleetOperation = "change-node-instance-type"
	// OperationChangeStorageClass changes the default class new volumes are created with.
	OperationChangeStorageClass FleetOperation = "change-storage-class"
	// OperationSwapNetworkPlugin replaces the CNI.
	OperationSwapNetworkPlugin FleetOperation = "swap-network-plugin"
	// OperationSwapIngressController replaces the ingress controller.
	OperationSwapIngressController FleetOperation = "swap-ingress-controller"
	// OperationUpgradeKubernetes moves the control plane to a new minor version.
	OperationUpgradeKubernetes FleetOperation = "upgrade-kubernetes"
)

// fleetConsequences classifies every operation. This table IS the governance model for
// runtime changes — everything else in the file reads from it.
var fleetConsequences = map[FleetOperation]Consequence{
	// Scale back down and the pool is what it was. The floor caps the size, so an ungated
	// scale cannot run away; the cap and the gate compose, which is why a light gate here is
	// not a hole.
	OperationScaleNodePool: ConsequenceReversible,
	// Drain and replace: hours of workload movement, but the old shape can be restored the
	// same way it was left.
	OperationChangeNodeInstanceType: ConsequenceDisruptive,
	// A new cluster is real infrastructure, real cost and a new governed surface — but an
	// empty cluster can be deleted, so this is not one-way.
	OperationRegisterCluster: ConsequenceDisruptive,
	// Volumes that already exist keep the class they were created with. Changing it does not
	// migrate data; it splits the estate into two classes, and the only way back through the
	// old data is a copy.
	OperationChangeStorageClass: ConsequenceIrreversible,
	// Every app's networking at once, and the swap is not transactional.
	OperationSwapNetworkPlugin:     ConsequenceIrreversible,
	OperationSwapIngressController: ConsequenceIrreversible,
	// etcd migrations are one-way. There is no downgrade; the rollback is a new cluster and
	// every workload migrated onto it.
	OperationUpgradeKubernetes: ConsequenceIrreversible,
	// Workloads must be evacuated first, and whatever was on the local disks is gone.
	OperationRemoveCluster: ConsequenceIrreversible,
}

// ConsequenceOf reports how far an operation can be walked back, and whether it is known at all.
func ConsequenceOf(operation FleetOperation) (Consequence, bool) {
	consequence, known := fleetConsequences[operation]
	return consequence, known
}

// TierForFleetOperation is the ONE place a fleet operation becomes a consequence tier, and so
// (via [GateFor]) the one place it becomes human attention.
//
// Two inputs, because both matter: WHAT the operation does to the runtime, and WHOSE runtime it
// is. A Kubernetes upgrade is one-way on a playground too, but a playground is disposable;
// scaling is trivial everywhere, but on production it is still worth a light look.
//
//	irreversible → prod          the strictest gate whatever the cluster is: no approval that
//	                             comes after can undo it, so the approval must come before
//	disruptive   → cluster tier  the cluster's own blast radius, unmodified
//	reversible   → one step lighter than the cluster tier
//
// Lowering for a reversible operation is NOT the bypass that [Manifest.validate] refuses. There
// a TEAM asked for a lighter tier; here the classification is code that no manifest can reach,
// and the magnitude is still bounded by the floor. Refusing to lower would mean scaling a
// production pool costs a platform approval — which is exactly the ceremony teams route around.
func TierForFleetOperation(operation FleetOperation, cluster Tier) Tier {
	consequence, known := fleetConsequences[operation]
	if !known {
		// An operation nobody classified gets the strictest answer. Failing open here would
		// mean a new operation ships ungoverned by omission.
		return TierProd
	}
	switch consequence {
	case ConsequenceIrreversible:
		return TierProd
	case ConsequenceDisruptive:
		return cluster
	default:
		return oneStepLighter(cluster)
	}
}

func oneStepLighter(tier Tier) Tier {
	switch tier {
	case TierProd:
		return TierShared
	default:
		return TierDev
	}
}

// kubernetesVersion is the shape a version may take: 1.29 or 1.29.4, never "latest" or
// "stable". A moving pointer here is the same failure as a container tag — approving it
// approves whatever it means next month.
var kubernetesVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// minorLine reduces 1.29.4 to 1.29. The minor line is what a floor allows and what an upgrade
// changes; the patch is applied by the provider and is a different question entirely.
func minorLine(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

// minorRank orders minor lines so a DOWNGRADE can be refused. An unparsable version sorts to
// -1; callers have already shape-checked what they pass.
func minorRank(version string) int {
	parts := strings.SplitN(minorLine(version), ".", 2)
	if len(parts) != 2 {
		return -1
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return -1
	}
	return major*1000 + minor
}

// FleetManifest is ONE cluster as a team declares it.
//
// Same sealed-floor discipline as [Manifest]: there is no maximum-nodes, no allowed-versions
// and no cost-cap field anywhere on this type, and [ParseFleetManifest] refuses unknown fields.
// A fleet manifest that tries to name its own ceiling fails to PARSE — it is not quietly
// ignored, which is how somebody comes to believe they set a limit that was never applied.
type FleetManifest struct {
	// Cluster is the cluster's name, and its identity in every slot below.
	Cluster string `json:"cluster"`
	// Team is the platform team answerable for this cluster.
	Team string `json:"team"`
	// Tier is the cluster's blast radius. REQUIRED — see [ParseFleetManifest] for why this one
	// has no default.
	Tier Tier `json:"tier"`
	// KubernetesVersion is the control plane's version. The floor decides which are allowed.
	KubernetesVersion string `json:"kubernetesVersion"`
	// StorageClass is the default class new volumes are created with.
	StorageClass string `json:"storageClass"`
	// NetworkPlugin is the CNI.
	NetworkPlugin string `json:"networkPlugin"`
	// IngressController is optional: a cluster may have none.
	IngressController string     `json:"ingressController,omitempty"`
	NodePools         []NodePool `json:"nodePools,omitempty"`
}

// NodePool is one pool of machines. Size is a REQUEST, not a limit — the ceiling lives in the
// floor and is not nameable here.
type NodePool struct {
	Name string `json:"name"`
	Size int    `json:"size"`
	// InstanceType is the machine shape. Declared rather than defaulted: the wrong shape is a
	// cost incident on one side and an OOM-killed workload on the other, and there is no
	// default that is right for both.
	InstanceType string `json:"instanceType"`
}

// FleetFloor is what the runtime may become. Server config, unreachable from a manifest.
//
// It bounds MAGNITUDE and AVAILABILITY: how big a pool may get, which machine shapes exist,
// which Kubernetes versions this deployment actually patches. It deliberately carries no
// allow-list for storage classes or CNIs — those have no magnitude for a floor to bound, and
// they are already held by the strictest gate there is because they are irreversible. A floor
// that tried to enumerate every string in the estate would rot into a registry nobody updates.
type FleetFloor struct {
	NodePool map[Tier]NodePoolLimits `json:"nodePool"`
	// KubernetesVersions is the set of MINOR lines this deployment supports. An unlisted
	// version is refused rather than defaulted: a version nobody patches is a fleet-wide CVE
	// with a date on it, and silently substituting a supported one would upgrade a cluster
	// nobody asked to upgrade.
	KubernetesVersions []string `json:"kubernetesVersions"`
}

// NodePoolLimits bound a pool in both directions.
type NodePoolLimits struct {
	// MinNodes stops a scale-to-zero that reads as a cost saving and lands as an outage. A
	// floor is not only a ceiling.
	MinNodes int `json:"minNodes"`
	MaxNodes int `json:"maxNodes"`
	// AllowedInstanceTypes is what this tier may run on. Without it, "change the instance
	// type" is an unbounded spend authorised by a light gate.
	AllowedInstanceTypes []string `json:"allowedInstanceTypes"`
}

// DefaultFleetFloor is a starting floor that scales with harm. A deployment overrides these
// values; what it cannot do is remove the floor.
func DefaultFleetFloor() FleetFloor {
	return FleetFloor{
		NodePool: map[Tier]NodePoolLimits{
			// A playground may go to zero — that IS the cost saving, and nothing depends on it.
			TierDev: {MinNodes: 0, MaxNodes: 5,
				AllowedInstanceTypes: []string{"standard-2", "standard-4"}},
			TierShared: {MinNodes: 1, MaxNodes: 20,
				AllowedInstanceTypes: []string{"standard-2", "standard-4", "standard-8"}},
			// Three is the smallest pool that survives losing a node while still spreading a
			// replica set; below that, one drain is an outage.
			TierProd: {MinNodes: 3, MaxNodes: 50,
				AllowedInstanceTypes: []string{"standard-4", "standard-8", "standard-16"}},
		},
		KubernetesVersions: []string{"1.28", "1.29"},
	}
}

// ControlPlaneLimits travels with a control-plane change so the approver and the adapter can
// both see what the floor allowed, rather than having to go and ask.
type ControlPlaneLimits struct {
	AllowedKubernetesVersions []string `json:"allowedKubernetesVersions"`
}

// ParseFleetManifest decodes and validates a cluster declaration.
//
// Unknown fields are REFUSED, which is the sealed floor expressed at the only place a manifest
// touches the system. The tier is REQUIRED rather than defaulted, and that is the one place
// this parser is stricter than [ParseManifest]: for an app footprint "unstated means
// playground" is safe because the blast radius is one team, but a cluster whose tier nobody
// declared is a substrate of unknown reach, and defaulting it to the lightest gate is exactly
// the failure this file exists to prevent.
func ParseFleetManifest(document []byte) (FleetManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()

	var manifest FleetManifest
	if err := decoder.Decode(&manifest); err != nil {
		return FleetManifest{}, fmt.Errorf("fleet manifest: %w", err)
	}
	if err := manifest.validate(); err != nil {
		return FleetManifest{}, err
	}
	return manifest, nil
}

func (f FleetManifest) validate() error {
	if !name.MatchString(f.Cluster) {
		return fmt.Errorf("fleet manifest: cluster %q is not a valid name", f.Cluster)
	}
	if !name.MatchString(f.Team) {
		return fmt.Errorf("fleet manifest: team %q is not a valid name", f.Team)
	}
	if f.Tier == "" {
		return fmt.Errorf(
			"fleet manifest: cluster %q must declare a tier — a cluster is the substrate every "+
				"app on it stands on, and an undeclared blast radius must not default to the "+
				"lightest gate", f.Cluster)
	}
	if !f.Tier.valid() {
		return fmt.Errorf("fleet manifest: tier %q is not one of dev, shared, prod", f.Tier)
	}
	if !kubernetesVersion.MatchString(f.KubernetesVersion) {
		return fmt.Errorf(
			"fleet manifest: cluster %q has kubernetes version %q — a version must be pinned "+
				"(1.29 or 1.29.4), because a moving pointer approves whatever it means next month",
			f.Cluster, f.KubernetesVersion)
	}
	if !name.MatchString(f.StorageClass) {
		return fmt.Errorf("fleet manifest: storage class %q is not a valid name", f.StorageClass)
	}
	if !name.MatchString(f.NetworkPlugin) {
		return fmt.Errorf("fleet manifest: network plugin %q is not a valid name", f.NetworkPlugin)
	}
	if f.IngressController != "" && !name.MatchString(f.IngressController) {
		return fmt.Errorf("fleet manifest: ingress controller %q is not a valid name",
			f.IngressController)
	}
	seen := make(map[string]bool, len(f.NodePools))
	for _, pool := range f.NodePools {
		if !name.MatchString(pool.Name) {
			return fmt.Errorf("fleet manifest: node pool %q is not a valid name", pool.Name)
		}
		if seen[pool.Name] {
			// Two pools of one name are two slots with one identity: the reconciler would see
			// each as drift from the other and flip the cluster between them forever.
			return fmt.Errorf("fleet manifest: node pool %q is declared twice", pool.Name)
		}
		seen[pool.Name] = true
		if pool.Size < 0 {
			return fmt.Errorf("fleet manifest: node pool %q has size %d", pool.Name, pool.Size)
		}
		if !name.MatchString(pool.InstanceType) {
			return fmt.Errorf("fleet manifest: node pool %q has instance type %q, which is not "+
				"a valid name", pool.Name, pool.InstanceType)
		}
	}
	return nil
}

// allows reports whether the floor supports this Kubernetes minor line.
func (f FleetFloor) allows(version string) bool {
	for _, allowed := range f.KubernetesVersions {
		if minorLine(allowed) == minorLine(version) {
			return true
		}
	}
	return false
}

func (l NodePoolLimits) allowsInstanceType(instanceType string) bool {
	for _, allowed := range l.AllowedInstanceTypes {
		if allowed == instanceType {
			return true
		}
	}
	return false
}

// ResolveFleet applies the floor to a cluster declaration and produces the changes the door
// rules on and the reconciler compares reality against.
//
// The items are the same [DesiredItem] the app path emits, so [Diff], [Reconcile] and the
// audit chain need to learn nothing about clusters. Asset is "fleet"; the kinds are the three
// things a fleet operation can touch.
//
// Each item carries the gate of the HEAVIEST operation that can mutate it, not the lightest.
// That matters for correction rather than for requests: [Drift.Correctable] auto-applies only
// what carried no gate, so gating a node pool at the drain-and-replace level is what stops a
// reconciler from silently replacing a cluster's machines to "fix" an instance type somebody
// changed by hand. The cheap gate for a cheap operation is expressed where a cheap operation
// actually arrives — [FleetChange].
func ResolveFleet(cluster FleetManifest, floor Floor) (Desired, error) {
	if err := cluster.validate(); err != nil {
		return Desired{}, err
	}
	poolLimits, ok := floor.Fleet.NodePool[cluster.Tier]
	if !ok {
		return Desired{}, fmt.Errorf("resolve fleet: no node pool floor configured for tier %q",
			cluster.Tier)
	}
	if !floor.Fleet.allows(cluster.KubernetesVersion) {
		return Desired{}, fmt.Errorf(
			"resolve fleet: cluster %q asks for kubernetes %q, which this deployment does not "+
				"support (%s) — an unsupported version is refused, never substituted, because "+
				"substituting one would upgrade a cluster nobody asked to upgrade",
			cluster.Cluster, cluster.KubernetesVersion,
			strings.Join(floor.Fleet.KubernetesVersions, ", "))
	}

	desired := Desired{Team: cluster.Team, Owns: cluster.Cluster}
	add := func(item DesiredItem) { desired.Changes = append(desired.Changes, item) }

	// The cluster itself: its existence, plus the cluster-wide properties that are all
	// irreversible and therefore all carry the same gate. No Limits — a storage class has no
	// magnitude for a floor to bound; it is bounded by the gate.
	clusterTier := TierForFleetOperation(OperationChangeStorageClass, cluster.Tier)
	clusterState := map[string]string{
		"storageClass":  cluster.StorageClass,
		"networkPlugin": cluster.NetworkPlugin,
	}
	if cluster.IngressController != "" {
		clusterState["ingressController"] = cluster.IngressController
	}
	add(DesiredItem{
		Asset: "fleet", Kind: "cluster", Name: cluster.Cluster,
		Slot: Slot{Cluster: cluster.Cluster, Name: "cluster"},
		Tier: clusterTier, Gate: floor.GateFor(clusterTier), Cluster: cluster.Cluster,
		State: clusterState,
	})

	controlPlaneState := map[string]string{"kubernetesVersion": cluster.KubernetesVersion}
	// A patch is recorded only if the manifest pinned one. Declaring a bare minor line means
	// "the provider's patch is fine", and inventing a patch to compare against would report
	// drift on every provider patch window forever.
	if strings.Count(cluster.KubernetesVersion, ".") == 2 {
		controlPlaneState["kubernetesPatch"] = cluster.KubernetesVersion
	}
	upgradeTier := TierForFleetOperation(OperationUpgradeKubernetes, cluster.Tier)
	add(DesiredItem{
		Asset: "fleet", Kind: "control-plane", Name: cluster.Cluster + "/control-plane",
		Slot: Slot{Cluster: cluster.Cluster, Name: "control-plane"},
		Tier: upgradeTier, Gate: floor.GateFor(upgradeTier), Cluster: cluster.Cluster,
		Limits: ControlPlaneLimits{AllowedKubernetesVersions: floor.Fleet.KubernetesVersions},
		State:  controlPlaneState,
	})

	poolTier := TierForFleetOperation(OperationChangeNodeInstanceType, cluster.Tier)
	for _, pool := range cluster.NodePools {
		if pool.Size > poolLimits.MaxNodes {
			return Desired{}, fmt.Errorf(
				"resolve fleet: node pool %q asks for %d nodes, above the %s floor of %d — the "+
					"request is refused rather than clamped, because a team told it has 50 "+
					"nodes when it has 20 plans capacity it does not have",
				pool.Name, pool.Size, cluster.Tier, poolLimits.MaxNodes)
		}
		if pool.Size < poolLimits.MinNodes {
			return Desired{}, fmt.Errorf(
				"resolve fleet: node pool %q asks for %d nodes, below the %s floor of %d — a "+
					"pool that small cannot survive draining one node",
				pool.Name, pool.Size, cluster.Tier, poolLimits.MinNodes)
		}
		if !poolLimits.allowsInstanceType(pool.InstanceType) {
			return Desired{}, fmt.Errorf(
				"resolve fleet: node pool %q asks for instance type %q, which the %s floor does "+
					"not allow (%s)",
				pool.Name, pool.InstanceType, cluster.Tier,
				strings.Join(poolLimits.AllowedInstanceTypes, ", "))
		}
		add(DesiredItem{
			Asset: "fleet", Kind: "node-pool", Name: cluster.Cluster + "/" + pool.Name,
			Slot: Slot{Cluster: cluster.Cluster, Name: pool.Name},
			Tier: poolTier, Gate: floor.GateFor(poolTier), Cluster: cluster.Cluster,
			Limits: poolLimits,
			State: map[string]string{
				"nodePoolSize":     strconv.Itoa(pool.Size),
				"instanceType":     pool.InstanceType,
				"nodePoolMaxNodes": strconv.Itoa(poolLimits.MaxNodes),
			},
		})
	}
	return desired, nil
}

// FleetChange is ONE requested change to the runtime — the daily act, and the thing the door
// actually rules on. [Promotion] is its equivalent on the app side, and for the same reason:
// the steady-state document says what should be true, while a request says what somebody wants
// to make true NOW, and only the second can be gated by its own consequence.
type FleetChange struct {
	Cluster   string         `json:"cluster"`
	Operation FleetOperation `json:"operation"`
	// NodePool names the pool for pool-scoped operations, empty for cluster-scoped ones.
	NodePool string `json:"nodePool,omitempty"`
	// To is the requested value: a node count for a scale, a version for an upgrade, a class
	// name for a storage change. ONE field rather than one per operation — the operation says
	// how to read it, and a struct with six mutually exclusive fields is six ways to send a
	// request that contradicts itself.
	To string `json:"to,omitempty"`
}

// AsChange turns a request into the one governed change an adapter would apply.
//
// The cluster's declaration is an argument rather than a field, deliberately: the tier and the
// current values come from what was approved for that cluster, never from the requester. A
// [FleetChange] carrying its own tier would be a request that chose its own gate.
func (change FleetChange) AsChange(cluster FleetManifest, floor Floor) (DesiredItem, error) {
	if err := cluster.validate(); err != nil {
		return DesiredItem{}, err
	}
	if change.Cluster != cluster.Cluster {
		return DesiredItem{}, fmt.Errorf(
			"fleet change: asks for cluster %q but was resolved against %q — governing a change "+
				"against the wrong cluster's tier is how a production change gets a dev gate",
			change.Cluster, cluster.Cluster)
	}
	if _, known := ConsequenceOf(change.Operation); !known {
		return DesiredItem{}, fmt.Errorf(
			"fleet change: operation %q is not classified — an operation whose consequence "+
				"nobody declared cannot be gated by it", change.Operation)
	}
	poolLimits, ok := floor.Fleet.NodePool[cluster.Tier]
	if !ok {
		return DesiredItem{}, fmt.Errorf("fleet change: no node pool floor for tier %q", cluster.Tier)
	}

	tier := TierForFleetOperation(change.Operation, cluster.Tier)
	item := DesiredItem{Asset: "fleet", Cluster: cluster.Cluster, Tier: tier, Gate: floor.GateFor(tier)}

	switch change.Operation {
	case OperationScaleNodePool, OperationChangeNodeInstanceType:
		pool, found := cluster.pool(change.NodePool)
		if !found {
			return DesiredItem{}, fmt.Errorf(
				"fleet change: cluster %q has no node pool %q", cluster.Cluster, change.NodePool)
		}
		item.Kind, item.Name = "node-pool", cluster.Cluster+"/"+pool.Name
		item.Slot = Slot{Cluster: cluster.Cluster, Name: pool.Name}
		item.Limits = poolLimits
		item.State = map[string]string{
			"nodePoolSize":     strconv.Itoa(pool.Size),
			"instanceType":     pool.InstanceType,
			"nodePoolMaxNodes": strconv.Itoa(poolLimits.MaxNodes),
		}
		if change.Operation == OperationScaleNodePool {
			size, err := strconv.Atoi(change.To)
			if err != nil {
				return DesiredItem{}, fmt.Errorf(
					"fleet change: scale %q wants %q nodes, which is not a number",
					pool.Name, change.To)
			}
			if size > poolLimits.MaxNodes || size < poolLimits.MinNodes {
				return DesiredItem{}, fmt.Errorf(
					"fleet change: scale %q to %d is outside the %s floor of %d..%d — the floor "+
						"is what makes a light gate on a reversible change safe",
					pool.Name, size, cluster.Tier, poolLimits.MinNodes, poolLimits.MaxNodes)
			}
			item.State["nodePoolSize"] = strconv.Itoa(size)
			break
		}
		if !poolLimits.allowsInstanceType(change.To) {
			return DesiredItem{}, fmt.Errorf(
				"fleet change: instance type %q is not allowed at tier %s (%s)",
				change.To, cluster.Tier, strings.Join(poolLimits.AllowedInstanceTypes, ", "))
		}
		item.State["instanceType"] = change.To

	case OperationUpgradeKubernetes:
		if !kubernetesVersion.MatchString(change.To) {
			return DesiredItem{}, fmt.Errorf(
				"fleet change: %q is not a pinned kubernetes version", change.To)
		}
		if !floor.Fleet.allows(change.To) {
			return DesiredItem{}, fmt.Errorf(
				"fleet change: kubernetes %q is not supported by this deployment (%s) — an "+
					"unlisted version is refused, not defaulted to the nearest supported one",
				change.To, strings.Join(floor.Fleet.KubernetesVersions, ", "))
		}
		if minorRank(change.To) < minorRank(cluster.KubernetesVersion) {
			return DesiredItem{}, fmt.Errorf(
				"fleet change: cluster %q is on kubernetes %s and asks for %s — a downgrade is "+
					"not a rollback: etcd migrations are one-way, so the only way back is a new "+
					"cluster with every workload migrated onto it",
				cluster.Cluster, cluster.KubernetesVersion, change.To)
		}
		item.Kind, item.Name = "control-plane", cluster.Cluster+"/control-plane"
		item.Slot = Slot{Cluster: cluster.Cluster, Name: "control-plane"}
		item.Limits = ControlPlaneLimits{AllowedKubernetesVersions: floor.Fleet.KubernetesVersions}
		item.State = map[string]string{"kubernetesVersion": change.To}

	case OperationChangeStorageClass, OperationSwapNetworkPlugin, OperationSwapIngressController:
		if !name.MatchString(change.To) {
			return DesiredItem{}, fmt.Errorf("fleet change: %q is not a valid name", change.To)
		}
		item.Kind, item.Name = "cluster", cluster.Cluster
		item.Slot = Slot{Cluster: cluster.Cluster, Name: "cluster"}
		item.State = map[string]string{
			"storageClass":  cluster.StorageClass,
			"networkPlugin": cluster.NetworkPlugin,
		}
		if cluster.IngressController != "" {
			item.State["ingressController"] = cluster.IngressController
		}
		item.State[fieldFor(change.Operation)] = change.To

	case OperationRegisterCluster, OperationRemoveCluster:
		item.Kind, item.Name = "cluster", cluster.Cluster
		item.Slot = Slot{Cluster: cluster.Cluster, Name: "cluster"}
		// clusterLifecycle is carried for the adapter and the audit record, and is deliberately
		// NEITHER governed nor watched: a cluster's existence is judged by the item being
		// absent or unexpected in [Diff], and a field for it as well would report one fact
		// twice and let the two answers disagree.
		lifecycle := "registered"
		if change.Operation == OperationRemoveCluster {
			lifecycle = "removed"
		}
		item.State = map[string]string{"clusterLifecycle": lifecycle}
	}

	return item, nil
}

func (f FleetManifest) pool(poolName string) (NodePool, bool) {
	for _, pool := range f.NodePools {
		if pool.Name == poolName {
			return pool, true
		}
	}
	return NodePool{}, false
}

// fieldFor maps a cluster-wide operation to the state field it writes.
func fieldFor(operation FleetOperation) string {
	switch operation {
	case OperationSwapNetworkPlugin:
		return "networkPlugin"
	case OperationSwapIngressController:
		return "ingressController"
	default:
		return "storageClass"
	}
}

// FleetOwnership declares which fleet fields MantleKeep governs and which belong to the runtime.
//
// The split is the same one the app path makes — MantleKeep decides WHAT the runtime is, the
// runtime decides HOW MUCH of it is running at any moment — and it is what stops the drift
// report from firing on legitimate autoscaling until somebody mutes it.
func FleetOwnership() Ownership {
	return Ownership{
		Governed: map[string]bool{
			// One-way. An unapproved minor upgrade cannot be undone by any approval that comes
			// after it, which is exactly the case a governance system exists for.
			"kubernetesVersion": true,
			// Existing volumes keep the class they were made with, so a silent change quietly
			// splits the estate in two and only new data notices.
			"storageClass": true,
			// Every app's networking at once.
			"networkPlugin":     true,
			"ingressController": true,
			// Nothing else legitimately changes the machine shape: an autoscaler adds nodes of
			// the CONFIGURED type, it never picks a different one. So a difference here is a
			// person who went around the door.
			"instanceType": true,
			// The pool's configured ceiling, which is the FLOOR's own number written onto the
			// cluster. If reality's ceiling is not the approved one, the floor itself has been
			// widened out of band — the one difference that invalidates every other guarantee
			// on this pool.
			"nodePoolMaxNodes": true,
		},
		Watched: map[string]bool{
			// The CURRENT node count belongs to the cluster autoscaler, which moves it up and
			// down all day for legitimate reasons. Correcting it would fight the autoscaler on
			// a timer, each seeing the other's value as drift.
			//
			// Watched is not ungoverned: a scale REQUEST still goes through the door and onto
			// the chain. Governing the ACT is a different thing from owning the FIELD, and
			// conflating the two is how a platform ends up either fighting the autoscaler or
			// letting scale changes happen with no record at all.
			"nodePoolSize": true,
			// Managed control planes apply PATCH versions on their own schedule and a cluster
			// cannot refuse them. Recording the move is useful; correcting it is impossible,
			// and treating it as a violation would cry wolf on every patch window.
			"kubernetesPatch": true,
		},
	}
}

// fleetDifferences compares the fleet-shaped state of two items.
//
// It takes the caller's compare function rather than building differences itself so the
// ownership and skip rules stay in ONE place — a second copy of "is this field governed" is a
// second copy that can disagree with the first.
//
// Keys are compared in sorted order: map iteration is random, and a drift report that
// reshuffles between runs is one nobody can compare with yesterday's.
func fleetDifferences(want DesiredItem, got ObservedItem, compare func(field, approved, observed string)) {
	if len(want.State) == 0 {
		return
	}
	fields := make([]string, 0, len(want.State))
	for field := range want.State {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		approved, observed := want.State[field], got.State[field]
		if field == "kubernetesVersion" {
			// Compared at the MINOR line, because that is what an upgrade changes and what the
			// floor allows. The patch is the provider's, and it has its own watched field.
			approved, observed = minorLine(approved), minorLine(observed)
		}
		compare(field, approved, observed)
	}
}
