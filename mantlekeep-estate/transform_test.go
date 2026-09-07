package estate

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// capturingPort keeps every change it was handed, whole and in order. The property under test
// is what the change LOOKED LIKE when it arrived, so a port that remembered only its name would
// answer a different question.
type capturingPort struct {
	asset   string
	applied []DesiredItem
	log     *[]string
}

func (p *capturingPort) Asset() string { return p.asset }

func (p *capturingPort) Observe(context.Context, string) (Observed, error) { return Observed{}, nil }

func (p *capturingPort) Apply(_ context.Context, _ mantlekeep.ExecutionToken, change DesiredItem) error {
	p.applied = append(p.applied, change)
	if p.log != nil {
		*p.log = append(*p.log, "apply "+change.Name)
	}
	return nil
}

// loggingDoor allows everything and records WHEN it was asked, against the same log the
// transform writes to. Order is the guarantee here, so both halves must be measured on one
// clock rather than inferred from two.
type loggingDoor struct {
	submitted []mantlekeep.Intent
	log       *[]string
}

func (d *loggingDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.submitted = append(d.submitted, intent)
	if d.log != nil {
		governed, _ := intent.Params["name"].(string)
		*d.log = append(*d.log, "door "+governed)
	}
	return mantlekeep.ExecutionToken{Value: "tok-" + intent.ID, IntentID: intent.ID,
		ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func transformFixture(t *testing.T, tier string) Manifest {
	t.Helper()
	manifest, err := ParseManifest([]byte(`{"team":"team-a","owns":"team-a","tier":"` + tier + `",
	    "kafka":{"cluster":"bus-one","topics":["one","two"]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return manifest
}

// With NO transform configured, the door and the adapter see exactly the resolved change.
//
// This is what makes the seam safe to add to a released module. Not "roughly the same" — the
// serialised change is compared against what [Resolve] produces, so a field quietly gained or a
// value quietly rewritten fails here.
func TestWithNoTransformTheDoorSeesExactlyTheResolvedChange(t *testing.T) {
	manifest := transformFixture(t, "dev")
	resolved, err := Resolve(manifest, DefaultFloor())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	port := &capturingPort{asset: "kafka"}
	manager := NewManager(&loggingDoor{}, DefaultFloor(), port)
	if _, err := manager.Apply(context.Background(), transformActor(), manifest); err != nil {
		t.Fatalf("apply: %v", err)
	}

	want, err := json.Marshal(resolved.Changes)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := json.Marshal(port.applied)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("an unconfigured manager changed what reaches the adapter\n want %s\n  got %s",
			want, got)
	}
}

// The ORDER is the guarantee: transform, then govern, then apply.
//
// A transform that ran after the door would mean a person approved a description and something
// else was produced from it afterwards — which is exactly the gap this seam exists to close.
func TestATransformRunsBeforeTheDoorAndItsOutputIsWhatIsGoverned(t *testing.T) {
	var log []string
	door := &loggingDoor{log: &log}
	port := &capturingPort{asset: "kafka", log: &log}

	manager := NewManager(door, DefaultFloor(), port).TransformChangesWith(
		ChangeTransformFunc(func(_ context.Context, team string, change DesiredItem) (DesiredItem, error) {
			log = append(log, "transform "+change.Name)
			if team != "team-a" {
				t.Errorf("the transform was told team %q", team)
			}
			change.State = map[string]string{"prepared": change.Name}
			return change, nil
		}))

	if _, err := manager.Apply(context.Background(), transformActor(),
		transformFixture(t, "dev")); err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, change := range port.applied {
		if change.State["prepared"] != change.Name {
			t.Fatalf("%s reached the adapter untransformed: %v", change.Name, change.State)
		}
	}
	// Every change: transformed, then submitted, then applied. Asserted per change rather than
	// globally, because a transform that ran for all of them AFTER the first submission would
	// still satisfy a "transform appears before door" check on the whole log.
	for _, change := range port.applied {
		transformedAt := indexIn(log, "transform "+change.Name)
		governedAt := indexIn(log, "door "+change.Name)
		appliedAt := indexIn(log, "apply "+change.Name)
		if transformedAt < 0 || governedAt < 0 || appliedAt < 0 {
			t.Fatalf("%s did not go through all three steps: %v", change.Name, log)
		}
		if !(transformedAt < governedAt && governedAt < appliedAt) {
			t.Fatalf("%s ran out of order (transform %d, door %d, apply %d): %v",
				change.Name, transformedAt, governedAt, appliedAt, log)
		}
	}
}

// A change that arrives ALREADY TRANSFORMED must not be transformed again.
//
// The approval path is where this matters. A second run would apply what the transform produces
// NOW under an approval given for what it produced THEN — the approval record would name a
// change nobody applied, and the audit trail would say it all went correctly.
func TestAnAlreadyTransformedChangeIsNotTransformedTwice(t *testing.T) {
	calls := 0
	door := &realisticDoor{}
	port := &capturingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	manager := NewManager(door, DefaultFloor(), port).AwaitApprovalIn(store).
		TransformChangesWith(ChangeTransformFunc(
			func(_ context.Context, _ string, change DesiredItem) (DesiredItem, error) {
				calls++
				change.State = map[string]string{"preparedOnCall": strconv.Itoa(calls)}
				return change, nil
			}))

	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "person-one"}, transformFixture(t, "prod"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Refused) == 0 || outcome.Refused[0].Approval == "" {
		t.Fatalf("the fixture must produce a gated change to approve: %+v", outcome)
	}
	requested := calls
	if requested == 0 {
		t.Fatal("the transform never ran on the request, so this test proves nothing about " +
			"it not running twice")
	}

	// What a person is asked to sign off is the TRANSFORMED change. If it were not, approving
	// would bind a signature to something other than what gets applied.
	pending, err := store.Get(context.Background(), outcome.Refused[0].Approval)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if pending.Change.State["preparedOnCall"] == "" {
		t.Fatalf("the stored approval holds an untransformed change: %+v", pending.Change)
	}

	result, err := manager.Approve(context.Background(),
		mantlekeep.Subject{ID: "person-two"}, pending.ID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !result.Applied() {
		t.Fatalf("the approved change did not apply: %+v", result)
	}
	if calls != requested {
		t.Fatalf("the transform ran %d more time(s) on the approval path — what is applied is "+
			"what the transform produces NOW, under an approval for what it produced THEN",
			calls-requested)
	}
	if len(port.applied) != 1 {
		t.Fatalf("expected exactly the approved change to reach the adapter; got %d",
			len(port.applied))
	}
	if port.applied[0].State["preparedOnCall"] != pending.Change.State["preparedOnCall"] {
		t.Fatalf("what was applied (%v) is not what was approved (%v)",
			port.applied[0].State, pending.Change.State)
	}
}

// A transform that fails refuses the change BEFORE the door — nothing is submitted, nothing is
// recorded as pending, and no adapter is called.
//
// Failing after the door would leave a decision on the chain for work that never happened, and
// an approval record somebody could act on for a change that cannot be produced.
func TestAFailingTransformStopsTheChangeBeforeTheDoor(t *testing.T) {
	door := &loggingDoor{}
	port := &capturingPort{asset: "kafka"}
	manager := NewManager(door, DefaultFloor(), port).TransformChangesWith(
		ChangeTransformFunc(func(_ context.Context, _ string, _ DesiredItem) (DesiredItem, error) {
			return DesiredItem{}, errors.New("the change could not be prepared")
		}))

	outcome, err := manager.Apply(context.Background(), transformActor(),
		transformFixture(t, "dev"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(door.submitted) != 0 {
		t.Fatalf("%d intent(s) reached the door after the transform failed — the chain now "+
			"carries a decision about work that cannot be produced", len(door.submitted))
	}
	if len(port.applied) != 0 {
		t.Fatalf("%d change(s) reached the adapter after the transform failed", len(port.applied))
	}
	if len(outcome.Failed) == 0 {
		t.Fatalf("a change that could not be prepared was not reported as failed: %+v", outcome)
	}
	if len(outcome.Applied) != 0 {
		t.Fatalf("something was reported applied: %+v", outcome.Applied)
	}
	// Reported as a failure, not a refusal. A refusal carries the door's own words and points
	// at an approval; here there is neither, and telling a caller to wait for a person who
	// cannot help is worse than saying nothing.
	for _, failed := range outcome.Failed {
		if failed.Approval != "" {
			t.Fatalf("a transform failure offered an approval to act on: %+v", failed)
		}
		if !strings.Contains(failed.Failed, "not submitted") {
			t.Fatalf("the failure must say the change never reached the door; got %q",
				failed.Failed)
		}
	}
}

// A transform may rewrite WHAT a change is; it may never rewrite WHICH change it is.
//
// Identity is what the door rules on and what the reconciler compares. A transform that moved it
// would produce an approval for one resource and an application to another, and both records
// would read as correct.
func TestATransformMayNotChangeWhichChangeThisIs(t *testing.T) {
	door := &loggingDoor{}
	port := &capturingPort{asset: "kafka"}
	manager := NewManager(door, DefaultFloor(), port).TransformChangesWith(
		ChangeTransformFunc(func(_ context.Context, _ string, change DesiredItem) (DesiredItem, error) {
			change.Name = change.Name + "-elsewhere"
			return change, nil
		}))

	outcome, err := manager.Apply(context.Background(), transformActor(),
		transformFixture(t, "dev"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(door.submitted) != 0 || len(port.applied) != 0 {
		t.Fatal("a transform moved a change to another identity and it was still governed")
	}
	if len(outcome.Failed) == 0 {
		t.Fatalf("the redirected change was not stopped: %+v", outcome)
	}
	if !strings.Contains(outcome.Failed[0].Failed, "identity") {
		t.Fatalf("the failure must name what went wrong; got %q", outcome.Failed[0].Failed)
	}
}

// The reconcile path governs through the same seam. A correction that skipped the transform
// would apply an un-prepared change on a timer, with nobody watching.
func TestTheReconcilePathTransformsToo(t *testing.T) {
	calls := 0
	door := &loggingDoor{}
	port := &capturingPort{asset: "kafka"}
	manager := NewManager(door, DefaultFloor(), port).TransformChangesWith(
		ChangeTransformFunc(func(_ context.Context, _ string, change DesiredItem) (DesiredItem, error) {
			calls++
			change.State = map[string]string{"prepared": "yes"}
			return change, nil
		}))

	// Nothing is observed, so every approved change is absent drift and correctable at dev tier.
	outcome, _, err := manager.Reconcile(context.Background(), transformActor(),
		transformFixture(t, "dev"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(outcome.Applied) == 0 {
		t.Fatalf("nothing was corrected, so nothing about the transform was exercised: %+v",
			outcome)
	}
	if calls != len(outcome.Applied) {
		t.Fatalf("the transform ran %d time(s) for %d correction(s)", calls, len(outcome.Applied))
	}
	for _, change := range port.applied {
		if change.State["prepared"] != "yes" {
			t.Fatalf("a correction reached the adapter un-prepared: %+v", change)
		}
	}
}

func transformActor() mantlekeep.Subject { return mantlekeep.Subject{ID: "person-one"} }

func indexIn(log []string, entry string) int {
	for index, line := range log {
		if line == entry {
			return index
		}
	}
	return -1
}
