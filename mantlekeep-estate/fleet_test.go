package estate

import (
	"context"
	"strings"
	"testing"
)

// sharedCluster is the fixture most tests read from: one cluster, one pool, nothing exotic.
func sharedCluster() FleetManifest {
	return FleetManifest{
		Cluster: "eu-shared-1", Team: "platform", Tier: TierShared,
		KubernetesVersion: "1.28", StorageClass: "gp3-encrypted", NetworkPlugin: "cilium",
		IngressController: "nginx",
		NodePools:         []NodePool{{Name: "general", Size: 4, InstanceType: "standard-4"}},
	}
}

func prodCluster() FleetManifest {
	cluster := sharedCluster()
	cluster.Cluster, cluster.Tier = "eu-prod-1", TierProd
	cluster.NodePools = []NodePool{{Name: "general", Size: 6, InstanceType: "standard-8"}}
	return cluster
}

// THE sealed floor, at the only place a manifest touches the system. A fleet manifest that can
// name its own ceiling has not been floored — it has been asked politely.
func TestAFleetManifestCannotNameItsOwnNodePoolMaximum(t *testing.T) {
	documents := map[string]string{
		"at the top level": `{
			"cluster":"eu-shared-1","team":"platform","tier":"shared",
			"kubernetesVersion":"1.28","storageClass":"gp3-encrypted","networkPlugin":"cilium",
			"maxNodesPerPool": 500
		}`,
		"inside a node pool": `{
			"cluster":"eu-shared-1","team":"platform","tier":"shared",
			"kubernetesVersion":"1.28","storageClass":"gp3-encrypted","networkPlugin":"cilium",
			"nodePools":[{"name":"general","size":4,"instanceType":"standard-4","maxSize":500}]
		}`,
	}

	for where, document := range documents {
		_, err := ParseFleetManifest([]byte(document))
		if err == nil {
			t.Fatalf("a manifest naming its own maximum %s was ACCEPTED — the limit is now "+
				"within reach of the request, which is the whole thing the floor exists to "+
				"prevent", where)
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("%s: the refusal must name the unknown field so the author sees WHICH key "+
				"was rejected; got %v", where, err)
		}
	}
}

// A cluster is the substrate every app on it stands on. "Unstated means playground" is safe for
// an app footprint and is not safe here.
func TestAClusterMustDeclareItsTierRatherThanDefaultToTheLightestGate(t *testing.T) {
	document := `{
		"cluster":"eu-shared-1","team":"platform",
		"kubernetesVersion":"1.28","storageClass":"gp3-encrypted","networkPlugin":"cilium"
	}`

	_, err := ParseFleetManifest([]byte(document))
	if err == nil {
		t.Fatal("a cluster with no tier parsed — an undeclared blast radius silently became the " +
			"lightest gate in the system")
	}
	if !strings.Contains(err.Error(), "must declare a tier") {
		t.Fatalf("the refusal must say what is missing; got %v", err)
	}
}

// A version that moves is a version nobody approved. Same failure as a container tag.
func TestAFleetManifestRefusesAMovingKubernetesVersion(t *testing.T) {
	document := `{
		"cluster":"eu-shared-1","team":"platform","tier":"shared",
		"kubernetesVersion":"latest","storageClass":"gp3-encrypted","networkPlugin":"cilium"
	}`

	if _, err := ParseFleetManifest([]byte(document)); err == nil {
		t.Fatal("kubernetes version \"latest\" was accepted — approving it approves whatever it " +
			"means next month")
	}
}

// THE POINT OF THE FILE. Same manifest, two operations, two consequences: scaling is undone by
// scaling back, a Kubernetes upgrade cannot be undone at all. Gating them the same way is what
// makes teams route around the platform.
func TestScalingIsALighterGateThanUpgradingKubernetes(t *testing.T) {
	floor := DefaultFloor()

	for _, cluster := range []FleetManifest{sharedCluster(), prodCluster()} {
		scale := FleetChange{Cluster: cluster.Cluster, Operation: OperationScaleNodePool,
			NodePool: "general", To: "5"}
		upgrade := FleetChange{Cluster: cluster.Cluster, Operation: OperationUpgradeKubernetes,
			To: "1.29"}

		scaleChange, err := scale.AsChange(cluster, floor)
		if err != nil {
			t.Fatalf("%s scale: %v", cluster.Tier, err)
		}
		upgradeChange, err := upgrade.AsChange(cluster, floor)
		if err != nil {
			t.Fatalf("%s upgrade: %v", cluster.Tier, err)
		}

		if upgradeChange.Gate != GatePlatform {
			t.Fatalf("%s: a one-way upgrade must cost a platform approval; got %q",
				cluster.Tier, upgradeChange.Gate)
		}
		if scaleChange.Tier.rank() >= upgradeChange.Tier.rank() {
			t.Fatalf("%s: scaling (%q) was gated as heavily as a one-way upgrade (%q) — tiering "+
				"every runtime change by the control plane rather than by the change is what over-gates "+
				"the reversible case", cluster.Tier, scaleChange.Tier, upgradeChange.Tier)
		}
	}

	// And concretely, so the table is pinned rather than merely ordered.
	expected := map[Tier]map[FleetOperation]Gate{
		TierDev: {
			OperationScaleNodePool:          GateNone,
			OperationChangeNodeInstanceType: GateNone,
			OperationUpgradeKubernetes:      GatePlatform,
			OperationChangeStorageClass:     GatePlatform,
			OperationRemoveCluster:          GatePlatform,
		},
		TierShared: {
			OperationScaleNodePool:          GateNone,
			OperationChangeNodeInstanceType: GateOwningTeam,
			OperationUpgradeKubernetes:      GatePlatform,
			OperationSwapNetworkPlugin:      GatePlatform,
		},
		TierProd: {
			OperationScaleNodePool:          GateOwningTeam,
			OperationChangeNodeInstanceType: GatePlatform,
			OperationUpgradeKubernetes:      GatePlatform,
			OperationSwapIngressController:  GatePlatform,
		},
	}
	for tier, operations := range expected {
		for operation, gate := range operations {
			if got := DefaultFloor().GateFor(TierForFleetOperation(operation, tier)); got != gate {
				t.Fatalf("%s cluster, %s: want gate %q, got %q", tier, operation, gate, got)
			}
		}
	}
}

// Fail closed. An operation nobody classified must not ship ungoverned by omission.
func TestAnUnclassifiedFleetOperationGetsTheStrictestGate(t *testing.T) {
	if tier := TierForFleetOperation("rewire-the-datacentre", TierDev); tier != TierProd {
		t.Fatalf("an unclassified operation resolved to %q — a new operation would ship "+
			"ungoverned because somebody forgot the table entry", tier)
	}
	if _, err := (FleetChange{Cluster: "eu-shared-1", Operation: "rewire-the-datacentre"}).
		AsChange(sharedCluster(), DefaultFloor()); err == nil {
		t.Fatal("an unclassified operation was turned into a change — its consequence is unknown, " +
			"so its gate is a guess")
	}
}

// The floor decides which versions exist. Substituting a supported one would upgrade a cluster
// nobody asked to upgrade.
func TestAnUnlistedKubernetesVersionIsRefusedRatherThanDefaulted(t *testing.T) {
	floor := DefaultFloor()
	cluster := sharedCluster()
	cluster.KubernetesVersion = "1.31"

	_, err := ResolveFleet(cluster, floor)
	if err == nil {
		t.Fatal("an unsupported kubernetes version resolved — either it was silently swapped " +
			"for a supported one, or the fleet now runs a version nobody patches")
	}
	if !strings.Contains(err.Error(), "1.31") {
		t.Fatalf("the refusal must name the version asked for; got %v", err)
	}

	upgrade := FleetChange{Cluster: cluster.Cluster, Operation: OperationUpgradeKubernetes, To: "1.31"}
	if _, err := upgrade.AsChange(sharedCluster(), floor); err == nil {
		t.Fatal("an upgrade to an unsupported version was accepted — the floor is reachable " +
			"through the request path even though it is not reachable through the manifest")
	}
}

// A downgrade is not a rollback. Accepting one would let an approval be granted on the belief
// that it can be reversed.
func TestAKubernetesDowngradeIsRefused(t *testing.T) {
	cluster := sharedCluster()
	cluster.KubernetesVersion = "1.29"
	downgrade := FleetChange{Cluster: cluster.Cluster, Operation: OperationUpgradeKubernetes, To: "1.28"}

	_, err := downgrade.AsChange(cluster, DefaultFloor())
	if err == nil {
		t.Fatal("a kubernetes downgrade was accepted — etcd migrations are one-way, so this " +
			"approval would be granted on a promise the platform cannot keep")
	}
	if !strings.Contains(err.Error(), "one-way") {
		t.Fatalf("the refusal must say why it cannot be undone; got %v", err)
	}
}

// Refused, not clamped. A team told it has 50 nodes when it has 20 plans capacity it does not
// have, and finds out under load.
func TestANodePoolAboveTheFloorIsRefusedNotSilentlyClamped(t *testing.T) {
	cluster := sharedCluster()
	cluster.NodePools = []NodePool{{Name: "general", Size: 500, InstanceType: "standard-4"}}

	desired, err := ResolveFleet(cluster, DefaultFloor())
	if err == nil {
		t.Fatalf("a 500-node pool at shared tier resolved to %+v", desired.Changes)
	}
	if !strings.Contains(err.Error(), "refused rather than clamped") {
		t.Fatalf("the refusal must say it is a refusal; got %v", err)
	}

	// The same ceiling has to hold on the request path, or the floor is only a parsing rule.
	scale := FleetChange{Cluster: cluster.Cluster, Operation: OperationScaleNodePool,
		NodePool: "general", To: "500"}
	if _, err := scale.AsChange(sharedCluster(), DefaultFloor()); err == nil {
		t.Fatal("a scale to 500 nodes was accepted — the light gate on a reversible change is " +
			"only safe because the floor bounds the magnitude")
	}
}

// A production pool that can be scaled to zero is a cost saving that lands as an outage. A
// floor is not only a ceiling.
func TestAProductionPoolCannotBeScaledToZero(t *testing.T) {
	scale := FleetChange{Cluster: "eu-prod-1", Operation: OperationScaleNodePool,
		NodePool: "general", To: "0"}

	if _, err := scale.AsChange(prodCluster(), DefaultFloor()); err == nil {
		t.Fatal("a production pool was scaled to zero — the floor bounded only the ceiling")
	}
}

// observedFleet mirrors what an adapter would read back from a real cluster.
func observedFleet(cluster, kind, itemName string, state map[string]string) ObservedItem {
	return ObservedItem{
		Asset: "fleet", Kind: kind, Name: cluster + "/" + itemName,
		Slot: Slot{Cluster: cluster, Name: itemName}, Cluster: cluster, State: state,
	}
}

// Two clusters are two slots. Without that, the reconciler compares one cluster's pool against
// another's and "fixes" a live cluster to match its neighbour.
func TestTwoClustersResolveToTwoSlotsAndDoNotCollide(t *testing.T) {
	floor := DefaultFloor()
	alpha := FleetManifest{Cluster: "alpha-dev", Team: "platform", Tier: TierDev,
		KubernetesVersion: "1.28", StorageClass: "gp3-encrypted", NetworkPlugin: "cilium",
		NodePools: []NodePool{{Name: "general", Size: 2, InstanceType: "standard-2"}}}
	beta := alpha
	beta.Cluster = "beta-dev"
	// Deliberately DIFFERENT, so a collision on the key cannot pass unnoticed: if both pools
	// shared one identity, one would be compared against the other's machine shape.
	beta.NodePools = []NodePool{{Name: "general", Size: 2, InstanceType: "standard-4"}}

	desiredAlpha, err := ResolveFleet(alpha, floor)
	if err != nil {
		t.Fatalf("alpha: %v", err)
	}
	desiredBeta, err := ResolveFleet(beta, floor)
	if err != nil {
		t.Fatalf("beta: %v", err)
	}

	keys := make(map[string]bool)
	for _, change := range append(desiredAlpha.Changes, desiredBeta.Changes...) {
		if change.Slot.Empty() {
			t.Fatalf("every fleet change needs a slot, or two clusters share one identity: %+v",
				change)
		}
		if keys[change.key()] {
			t.Fatalf("two fleet changes share the key %q", change.key())
		}
		keys[change.key()] = true
	}
	if len(keys) != len(desiredAlpha.Changes)+len(desiredBeta.Changes) {
		t.Fatalf("want %d distinct keys, got %d",
			len(desiredAlpha.Changes)+len(desiredBeta.Changes), len(keys))
	}

	// And the whole fleet, observed exactly as approved, must be quiet.
	fleet := Desired{Team: "platform", Changes: append(desiredAlpha.Changes, desiredBeta.Changes...)}
	observed := Observed{Items: []ObservedItem{
		observedFleet("alpha-dev", "cluster", "cluster",
			map[string]string{"storageClass": "gp3-encrypted", "networkPlugin": "cilium"}),
		observedFleet("alpha-dev", "control-plane", "control-plane",
			map[string]string{"kubernetesVersion": "1.28"}),
		observedFleet("alpha-dev", "node-pool", "general", map[string]string{
			"nodePoolSize": "2", "instanceType": "standard-2", "nodePoolMaxNodes": "5"}),
		observedFleet("beta-dev", "cluster", "cluster",
			map[string]string{"storageClass": "gp3-encrypted", "networkPlugin": "cilium"}),
		observedFleet("beta-dev", "control-plane", "control-plane",
			map[string]string{"kubernetesVersion": "1.28"}),
		observedFleet("beta-dev", "node-pool", "general", map[string]string{
			"nodePoolSize": "2", "instanceType": "standard-4", "nodePoolMaxNodes": "5"}),
	}}

	if drifts := Diff(fleet, observed); len(drifts) != 0 {
		t.Fatalf("two clusters matching their approvals produced drift — their items collided; "+
			"got %+v", drifts)
	}
}

// Fixing the alpha-dev cluster's item names in one place: the two drift tests below both need
// a desired state and a matching observation to perturb.
func devFleet(t *testing.T) Desired {
	t.Helper()
	cluster := FleetManifest{Cluster: "alpha-dev", Team: "platform", Tier: TierDev,
		KubernetesVersion: "1.28", StorageClass: "gp3-encrypted", NetworkPlugin: "cilium",
		NodePools: []NodePool{{Name: "general", Size: 2, InstanceType: "standard-2"}}}
	desired, err := ResolveFleet(cluster, DefaultFloor())
	if err != nil {
		t.Fatalf("resolve fleet: %v", err)
	}
	return desired
}

// A Kubernetes version that is not the approved one is a VIOLATION: it is one-way, and it is
// the first question an auditor asks about a cluster.
func TestDriftOnAGovernedFleetFieldIsAViolation(t *testing.T) {
	desired := devFleet(t)
	observed := Observed{Items: []ObservedItem{
		observedFleet("alpha-dev", "cluster", "cluster",
			map[string]string{"storageClass": "gp3-encrypted", "networkPlugin": "cilium"}),
		observedFleet("alpha-dev", "control-plane", "control-plane",
			map[string]string{"kubernetesVersion": "1.29"}), // somebody upgraded it
		observedFleet("alpha-dev", "node-pool", "general", map[string]string{
			"nodePoolSize": "2", "instanceType": "standard-2", "nodePoolMaxNodes": "5"}),
	}}

	drifts := Diff(desired, observed)
	if len(drifts) != 1 {
		t.Fatalf("want exactly one drift, got %+v", drifts)
	}
	if !drifts[0].Ungoverned() {
		t.Fatal("an unapproved kubernetes upgrade was not treated as a violation — it cannot be " +
			"undone, so nothing that comes after this can put it right")
	}
	if len(drifts[0].Differences) != 1 || drifts[0].Differences[0].Field != "kubernetesVersion" {
		t.Fatalf("the drift must name the field; got %+v", drifts[0].Differences)
	}
	if !drifts[0].Differences[0].Governed {
		t.Fatal("the kubernetes version must be marked governed, or the report reads as a fact " +
			"about the platform rather than as a violation")
	}
	// Gated, therefore a decision rather than an action: a reconciler that quietly re-applied
	// the approved version would be attempting a Kubernetes DOWNGRADE on its own initiative.
	if drifts[0].Correctable() {
		t.Fatal("a control plane version drift was marked auto-correctable — the correction is " +
			"itself an irreversible operation")
	}
}

// THE anti-noise rule, at fleet scale. The cluster autoscaler owns the current node count and
// moves it all day; a report that fires on that is muted within a week, and the one real
// unapproved change goes unnoticed with everything else.
func TestDriftOnAWatchedFleetFieldIsRecordedNotCorrected(t *testing.T) {
	desired := devFleet(t)
	observed := Observed{Items: []ObservedItem{
		observedFleet("alpha-dev", "cluster", "cluster",
			map[string]string{"storageClass": "gp3-encrypted", "networkPlugin": "cilium"}),
		observedFleet("alpha-dev", "control-plane", "control-plane",
			map[string]string{"kubernetesVersion": "1.28"}),
		observedFleet("alpha-dev", "node-pool", "general", map[string]string{
			"nodePoolSize": "5", // the autoscaler, doing its job
			"instanceType": "standard-2", "nodePoolMaxNodes": "5"}),
	}}

	drifts := Diff(desired, observed)
	if len(drifts) != 1 {
		t.Fatalf("a watched difference must still be RECORDED; got %+v", drifts)
	}
	if drifts[0].Ungoverned() {
		t.Fatal("an autoscaled node count was treated as a violation — MantleKeep would fight the " +
			"cluster autoscaler on a timer, and the report would be muted")
	}
	if drifts[0].Correctable() {
		t.Fatal("an autoscaled node count was marked correctable")
	}
	if !strings.Contains(drifts[0].Detail, "watched") {
		t.Fatalf("the report must say whose field it is; got %q", drifts[0].Detail)
	}
}

// The pool's CEILING is ours even though its current size is not. If reality's ceiling is not
// the approved one, the floor itself was widened out of band — which invalidates every other
// guarantee on that pool.
func TestAWidenedNodePoolCeilingIsAViolationEvenThoughTheSizeIsNot(t *testing.T) {
	desired := devFleet(t)
	observed := Observed{Items: []ObservedItem{
		observedFleet("alpha-dev", "cluster", "cluster",
			map[string]string{"storageClass": "gp3-encrypted", "networkPlugin": "cilium"}),
		observedFleet("alpha-dev", "control-plane", "control-plane",
			map[string]string{"kubernetesVersion": "1.28"}),
		observedFleet("alpha-dev", "node-pool", "general", map[string]string{
			"nodePoolSize": "2", "instanceType": "standard-2", "nodePoolMaxNodes": "500"}),
	}}

	drifts := Diff(desired, observed)
	if len(drifts) != 1 || !drifts[0].Ungoverned() {
		t.Fatalf("a widened pool ceiling must be a violation; got %+v", drifts)
	}
}

// A managed control plane applies patches on its own schedule and cannot refuse them. The
// MINOR line is governed; the patch is watched. Treating the two the same would either cry
// wolf every patch window or stop noticing upgrades altogether.
func TestAProviderPatchIsWatchedWhileTheMinorLineIsGoverned(t *testing.T) {
	cluster := FleetManifest{Cluster: "alpha-dev", Team: "platform", Tier: TierDev,
		KubernetesVersion: "1.28.4", StorageClass: "gp3-encrypted", NetworkPlugin: "cilium"}
	desired, err := ResolveFleet(cluster, DefaultFloor())
	if err != nil {
		t.Fatalf("resolve fleet: %v", err)
	}
	observed := Observed{Items: []ObservedItem{
		observedFleet("alpha-dev", "cluster", "cluster",
			map[string]string{"storageClass": "gp3-encrypted", "networkPlugin": "cilium"}),
		observedFleet("alpha-dev", "control-plane", "control-plane", map[string]string{
			"kubernetesVersion": "1.28.9", "kubernetesPatch": "1.28.9"}),
	}}

	drifts := Diff(desired, observed)
	if len(drifts) != 1 {
		t.Fatalf("a provider patch must be recorded once; got %+v", drifts)
	}
	if drifts[0].Ungoverned() {
		t.Fatalf("a provider patch was treated as a violation — the cluster cannot refuse it, "+
			"so this fires on every patch window; got %+v", drifts[0].Differences)
	}
	for _, difference := range drifts[0].Differences {
		if difference.Field == "kubernetesVersion" {
			t.Fatal("the minor line was reported as changed by a PATCH — the governed field " +
				"must compare 1.28.4 against 1.28.9 at the 1.28 line, or every patch reads as " +
				"an unapproved upgrade")
		}
	}
}

// A fleet drift that needed a person to happen needs a person to fix. The reconciler must not
// walk a cluster back on its own.
func TestGatedFleetDriftIsEscalatedNeverAutoApplied(t *testing.T) {
	port := &fakePort{}
	desired := devFleet(t)
	drifts := Diff(desired, Observed{}) // nothing exists yet: every item is absent

	outcome := Reconcile(context.Background(), port, testToken("token-123"), drifts)

	for _, corrected := range outcome.Corrected {
		if corrected.Desired.Kind == "control-plane" || corrected.Desired.Kind == "cluster" {
			t.Fatalf("the reconciler applied an irreversible fleet change with no human: %q",
				corrected.Desired.Name)
		}
	}
	if len(outcome.Escalated) != 2 {
		t.Fatalf("the cluster and its control plane must both escalate; got %+v", outcome.Escalated)
	}
	// The node pool on a playground is nobody's emergency, and must still be automatic.
	if len(outcome.Corrected) != 1 || outcome.Corrected[0].Desired.Kind != "node-pool" {
		t.Fatalf("a dev node pool must be created without ceremony; got %+v", outcome.Corrected)
	}
}
