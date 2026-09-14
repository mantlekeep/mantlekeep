package serve

import (
	"context"
	"errors"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/config"
)

// allowAll is a door that permits everything, so the transform is the only thing that can stop a
// change. A refusing door would make these tests pass whether the option was wired or not.
type allowAll struct{}

func (allowAll) Submit(context.Context, mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	return mantlekeep.ExecutionToken{Value: "test", IntentID: "INT-1"}, nil
}

// seenPort records what reached the adapter — the only place "it happened" is true.
type seenPort struct{ applied []estate.DesiredItem }

func (p *seenPort) Asset() string { return "app" }
func (p *seenPort) Observe(context.Context, string) (estate.Observed, error) {
	return estate.Observed{}, nil
}
func (p *seenPort) Apply(_ context.Context, _ mantlekeep.ExecutionToken,
	change estate.DesiredItem) error {
	p.applied = append(p.applied, change)
	return nil
}

// THE test for this option: a Transform supplied in Options must actually reach the manager.
//
// The failure being guarded against is the quiet one — a field that exists, is documented, and is
// never read. Nothing about that looks wrong in review, the server still starts, and the
// deployment believes a check is running that is not.
func TestATransformSuppliedInOptionsReachesTheManager(t *testing.T) {
	refused := errors.New("refused by the deployment's own rule")
	port := &seenPort{}
	manager := managerForTest(Options{
		Ports: []estate.Port{port},
		Transform: estate.ChangeTransformerFunc(func(context.Context, string,
			estate.DesiredItem) (estate.DesiredItem, error) {
			return estate.DesiredItem{}, refused
		}),
	})

	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: requester}, appManifest())
	if err != nil {
		t.Fatalf(wrapApply, err)
	}
	if len(port.applied) != 0 {
		t.Fatalf("the transform refused the change and it reached the adapter anyway: %+v.\n"+
			"Options.Transform is set and the manager never received it", port.applied)
	}
	if len(outcome.Failed) != 1 {
		t.Fatalf("the refusal was not reported: %+v", outcome)
	}
}

// A nil Transform is the behaviour every deployment had before this option existed, and must stay
// that way: an upgrade that started refusing changes because a field was left unset would be an
// outage delivered as a patch release.
func TestANilTransformChangesNothing(t *testing.T) {
	port := &seenPort{}
	manager := managerForTest(Options{Ports: []estate.Port{port}})

	if _, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: requester}, appManifest()); err != nil {
		t.Fatalf(wrapApply, err)
	}
	if len(port.applied) != 1 {
		t.Fatalf("a manager with no transform applied %d changes, want 1", len(port.applied))
	}
}

// The requester every fixture here declares as. Named once: three call sites spelling the same
// person is three places a rename half-applies.
const requester = "dev-alice"

// wrapApply is the one error message these tests share, so the literal lives in one place.
const wrapApply = "apply: %v"

// gateReason is the door's own words, used by more than one fixture here.
const gateReason = "a production change needs a second person"

// gatingDoor refuses with a PENDING refusal — the shape the door uses to say "a person is needed"
// as opposed to "no".
type gatingDoor struct{}

func (gatingDoor) Submit(context.Context, mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	return mantlekeep.ExecutionToken{}, &mantlekeep.Refused{
		Action:            mantlekeep.ActionRequireApproval,
		Reason:            gateReason,
		RequiredApprovers: []mantlekeep.Role{mantlekeep.RoleArchitect},
	}
}

// managerForTest builds the manager the way Run does, through the same function, with everything
// Run reads from disk replaced by a fixture. Calling managerFor rather than rebuilding the chain
// is the point: a chain assembled twice can differ in the one place nobody is looking.
func managerForTest(options Options) *estate.Manager {
	return managerForTestWithDoor(allowAll{}, options)
}

func managerForTestWithDoor(door mantlekeep.Submitter, options Options) *estate.Manager {
	floor := testFloor()
	settings := config.Config{Floor: floor}
	store := estate.NewMemoryManifests()
	service := estate.NewService(floor, store, options.Ports...)
	placer := testPlacer()
	return managerFor(options, door, settings, service, store, approvalsFor(options),
		func() estate.Floor { return floor },
		func() *estate.Placer { return placer })
}

func testFloor() estate.Floor {
	return estate.Floor{App: map[estate.Runtime]map[estate.Tier]estate.AppLimits{
		"enterprise": {estate.TierDev: {}, estate.TierShared: {}, estate.TierProd: {}},
	}}
}

func testPlacer() *estate.Placer {
	return estate.NewPlacer([]estate.Cluster{
		{Name: "sit-a", Env: "sit", Purpose: "app", Residency: "region-a", Reachable: true},
	}).WithCapacity([]estate.Capacity{{Cluster: "sit-a", AllocatablePct: 0.60}})
}

func appManifest() estate.Manifest {
	return estate.Manifest{
		Team: "payments", Owns: "payments", Tier: estate.TierDev,
		Apps: []estate.App{{
			Name: "ledger", Runtime: "enterprise", Image: "registry.local/payments/ledger",
			Placement: estate.Placement{Env: "sit", Purpose: "app", Residency: "region-a"},
		}},
	}
}

// --- the approvals store ------------------------------------------------------------------------

// countingApprovals records whether the deployment's own store was used at all.
type countingApprovals struct {
	estate.Approvals
	opened int
}

func (c *countingApprovals) Open(ctx context.Context, approval estate.Approval) error {
	c.opened++
	return c.Approvals.Open(ctx, approval)
}

// A store supplied in Options must actually be the one gated changes wait in. The failure guarded
// against is the quiet one: a field that exists, is documented, and is never read — after which a
// deployment believes its approvals are durable and a restart still loses them.
func TestAnApprovalsStoreSuppliedInOptionsIsTheOneUsed(t *testing.T) {
	store := &countingApprovals{Approvals: estate.NewMemoryApprovals()}
	// A door that GATES. Forcing the gate on the change is not enough: the change waits only
	// because the DOOR says so, which is the whole point of the estate computing a gate and the
	// door ruling on it.
	manager := managerForTestWithDoor(gatingDoor{}, Options{
		Ports:     []estate.Port{&seenPort{}},
		Approvals: store,
	})

	if _, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: requester}, appManifest()); err != nil {
		t.Fatalf(wrapApply, err)
	}
	if store.opened == 0 {
		t.Fatal("the gated change was not written to the store this deployment supplied — " +
			"Options.Approvals is set and the manager never received it, so a restart loses " +
			"every pending approval while the deployment believes otherwise")
	}
}

// Nil keeps the previous behaviour exactly. An upgrade that refused to start because a new field
// was unset would be an outage delivered as a patch release.
func TestANilApprovalsStoreStillWorks(t *testing.T) {
	manager := managerForTest(Options{Ports: []estate.Port{&seenPort{}}})
	if _, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: requester}, appManifest()); err != nil {
		t.Fatalf("apply with no approvals store supplied: %v", err)
	}
}
