package estate_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/admissiontest"
)

// The shipped in-memory store must obey the contract it implements.
// Named because each is asserted in several cases here, and copies of a format string are
// copies to edit when the wording changes.
const (
	litAdmitting = "admitting: %v"
	litStoreDown = "dial tcp 10.0.0.7:5432: connect: connection refused"
)

func TestMemoryAdmissionsConformsToTheContract(t *testing.T) {
	admissiontest.Run(t, func(t *testing.T) estate.Admissions {
		return estate.NewMemoryAdmissions()
	})
}

// And the limit of that store, asserted rather than left in a comment.
//
// MemoryAdmissions guards Admit with a mutex, which is correct and sufficient inside ONE
// process. Two replicas hold two mutexes over two maps and coordinate nothing, so both accept an
// onboarding for the same app and environment, and the reference an auditor follows depends on
// which replica they ask. Worse than the approvals version of this bug, because an admission is
// a STANDING decision: the disagreement does not resolve itself the way a one-off approval does.
//
// This test creates two stores deliberately, because that is what a second replica IS. It exists
// so the deployment constraint is a failing test away from being noticed, rather than a line in
// a Helm chart somebody raises to 2 on a busy afternoon.
func TestTwoAdmissionStoresCannotCoordinate(t *testing.T) {
	replicaOne := estate.NewMemoryAdmissions()
	replicaTwo := estate.NewMemoryAdmissions()
	ctx := context.Background()

	var (
		mu       sync.Mutex
		accepted int
		wait     sync.WaitGroup
	)
	for index, replica := range []estate.Admissions{replicaOne, replicaTwo} {
		wait.Add(1)
		go func(n int, store estate.Admissions) {
			defer wait.Done()
			record := admissionFor("canary-tokyo")
			// Two different tickets, as two gatekeepers working from two queues would have.
			record.Reference = []string{"CHG-1001", "CHG-2002"}[n]
			if store.Admit(ctx, record) == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(index, replica)
	}
	wait.Wait()

	if accepted != 2 {
		t.Fatalf("expected BOTH replicas to accept — that is the bug being documented; got %d",
			accepted)
	}
	t.Log("two replicas each accepted an onboarding for the same app and environment under " +
		"different references: an auditor asking 'why is this here' gets a different answer " +
		"per replica. This is why estate runs a single replica until a store with " +
		"compare-and-set backs it.")
}

// THE demonstration: a change is refused because its app is not admitted, and the same change
// succeeds once it is.
//
// Without this the whole seam is a store nothing consults — and a control nothing calls is worse
// than no control, because the estate looks governed. It also proves the two halves belong to
// each other: the identity the refusal names is the identity the resolver produced, so an
// onboarding recorded against a name the resolver never uses would fail here rather than in
// production.
func TestAChangeIsRefusedUntilItsAppIsAdmitted(t *testing.T) {
	door := &allowingDoor{}
	apps := &recordingAppPort{}
	admissions := estate.NewMemoryAdmissions()
	manager := estate.NewManager(door, floorForOpaqueEnv(), apps).
		PlaceOn(placerForOpaqueEnv()).
		TransformChangesWith(estate.CheckAdmissionIn(admissions))

	// 1. Not onboarded. The change must not reach the adapter, and must not reach the door.
	outcome, err := manager.Apply(context.Background(), deployer(), manifestForOpaqueEnv(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(outcome.Applied) != 0 {
		t.Fatalf("an app nobody onboarded was deployed: %+v", outcome.Applied)
	}
	refusal := onlyRefusal(t, outcome)
	if !strings.Contains(refusal, "not admitted to environment \"canary-tokyo\"") {
		t.Fatalf("the refusal must name the app and the environment, got %q", refusal)
	}
	if len(apps.applied) != 0 {
		t.Fatalf("the adapter was called for an app nobody onboarded: %v", apps.applied)
	}
	if len(door.submitted) != 0 {
		t.Fatalf("the door was asked about a change that should have stopped before it: %d intents",
			len(door.submitted))
	}
	// Logged because the WORDING is the deliverable as much as the refusal is: this sentence is
	// what a deployer reads at 2am, and a reviewer should be able to read it here.
	t.Logf("before onboarding: %s", refusal)

	// 2. Onboarded, through the door, by a gatekeeper who is not the deployer.
	if err := estate.AdmitApp(context.Background(), door, admissions, gatekeeper(),
		admissionFor("canary-tokyo")); err != nil {
		t.Fatalf(litAdmitting, err)
	}

	// 3. The SAME change now applies.
	outcome, err = manager.Apply(context.Background(), deployer(), manifestForOpaqueEnv(t))
	if err != nil {
		t.Fatalf("apply after admission: %v", err)
	}
	if len(outcome.Applied) != 1 {
		t.Fatalf("the admitted app must deploy; applied=%+v refused=%+v failed=%+v",
			outcome.Applied, outcome.Refused, outcome.Failed)
	}
	if apps.applied["payments-checkout"] == "" {
		t.Fatalf("the adapter was never handed the admitted change: %v", apps.applied)
	}

	// 4. Revoked. The next deploy stops again — nothing here touches what is already running.
	if err := estate.RevokeApp(context.Background(), door, admissions, gatekeeper(),
		revocationFor("canary-tokyo")); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	outcome, err = manager.Apply(context.Background(), deployer(), manifestForOpaqueEnv(t))
	if err != nil {
		t.Fatalf("apply after revocation: %v", err)
	}
	if len(outcome.Applied) != 0 {
		t.Fatalf("a revoked app was deployed again: %+v", outcome.Applied)
	}
	afterRevocation := onlyRefusal(t, outcome)
	if !strings.Contains(afterRevocation, "the ledger migration finished") {
		t.Fatalf("the refusal must quote the revocation's own reason, got %q", afterRevocation)
	}
	t.Logf("after revocation: %s", afterRevocation)
}

// A refusal must carry the gatekeeper's words AND the reference, or it is a dead end.
//
// Without this the refusal degrades to "denied": the deployer cannot tell whether their app was
// never onboarded, was taken out on purpose, or lapsed — and cannot find anybody to ask. That is
// how people learn to route around a control instead of using it.
func TestARefusalNamesTheReasonAndTheReference(t *testing.T) {
	admissions := estate.NewMemoryAdmissions()
	ctx := context.Background()

	// Never onboarded: there is no reference to cite, so the refusal must say what CREATES one.
	err := estate.RequireAdmission(ctx, admissions, "payments", "payments-checkout", "canary-tokyo")
	if !errors.Is(err, estate.ErrAdmissionNotFound) {
		t.Fatalf("an app with no record must refuse as ErrAdmissionNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "governed act") {
		t.Fatalf("a refusal with no reference must say how one comes to exist, got %q", err)
	}

	// Revoked: the refusal must quote the revocation, not paraphrase it.
	if err := admissions.Admit(ctx, admissionFor("canary-tokyo")); err != nil {
		t.Fatalf(litAdmitting, err)
	}
	if err := admissions.Revoke(ctx, revocationFor("canary-tokyo")); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	err = estate.RequireAdmission(ctx, admissions, "payments", "payments-checkout", "canary-tokyo")
	if !errors.Is(err, estate.ErrNotAdmitted) {
		t.Fatalf("a revoked app must refuse as ErrNotAdmitted, got %v", err)
	}
	var refused *estate.NotAdmitted
	if !errors.As(err, &refused) {
		t.Fatalf("a refusal must be a *NotAdmitted a caller can read fields from, got %T", err)
	}
	if refused.Reference != "CHG-TOKYO-9902" || refused.Reason != revocationFor("canary-tokyo").Reason {
		t.Fatalf("the refusal must carry the revocation's reference and its own words, got %+v",
			refused)
	}
}

// An unreachable store REFUSES. It never assumes admitted.
//
// Without this the control inverts under exactly the conditions it matters most: a store outage
// would admit every app to every environment for as long as it lasted, and nothing in the record
// afterwards would distinguish those deploys from onboarded ones. The cost is stated rather than
// hidden — the store is a HARD DEPENDENCY, and with it down nothing deploys.
func TestAdmissionFailsClosedWhenTheStoreCannotAnswer(t *testing.T) {
	ctx := context.Background()

	// A store that cannot answer.
	err := estate.RequireAdmission(ctx, unreachableAdmissions{}, "payments", "payments-checkout",
		"canary-tokyo")
	if !errors.Is(err, estate.ErrAdmissionStoreUnreachable) {
		t.Fatalf("an erroring store must refuse, got %v", err)
	}

	// No store configured at all — a mis-wiring, which must also refuse rather than pass.
	if estate.RequireAdmission(ctx, nil, "payments", "payments-checkout", "canary-tokyo") == nil {
		t.Fatal("with no admission store configured, every app was admitted everywhere")
	}

	// And a change that names no environment: treating an empty env as "any" would admit an app
	// to every environment at once.
	if estate.RequireAdmission(ctx, estate.NewMemoryAdmissions(), "payments",
		"payments-checkout", "") == nil {
		t.Fatal("a change with no environment was admitted")
	}
}

// Admission and the gate are separate questions, and neither answers the other.
//
// Without this the two collapse the first time somebody is under pressure: an admission starts
// waving a change past its approval ("it is onboarded, so it is approved"), or an approval starts
// admitting an app ("a person signed it, so it belongs here"). Either direction loses a control
// that nothing else provides.
func TestAdmissionIsNotTheGate(t *testing.T) {
	ctx := context.Background()
	admissions := estate.NewMemoryAdmissions()
	if err := admissions.Admit(ctx, admissionFor("canary-tokyo")); err != nil {
		t.Fatalf(litAdmitting, err)
	}

	// An admitted app in a gated environment is still gated: admission did not lower the gate.
	floor := floorForOpaqueEnv()
	floor.EnvTiers = map[string]estate.Tier{"canary-tokyo": estate.TierProd}
	if gate := floor.GateForApp(estate.TierProd, "payments/checkout"); gate != estate.GatePlatform {
		t.Fatalf("admission must not change what the gate costs; got %q", gate)
	}

	// And an UNGATED change is still refused when its app is not admitted — proven in
	// TestAChangeIsRefusedUntilItsAppIsAdmitted, where the tier is dev and the gate is none.
	// Restated here as the other direction: the gate says yes, admission says no, and no wins.
	if floorForOpaqueEnv().GateFor(estate.TierDev) != estate.GateNone {
		t.Fatal("this case assumes the opaque-env floor leaves dev ungated")
	}
	if estate.RequireAdmission(ctx, admissions, "payments", "payments-ledger",
		"canary-tokyo") == nil {
		t.Fatal("an ungated change was admitted for an app nobody onboarded")
	}
}

// Onboarding is GOVERNED: the door decides, and only then is the record written.
//
// Without this the onboarding record is a spreadsheet in a mutex — anybody who can reach the
// store can admit an app anywhere, and the chain never hears about it. Worse, writing first and
// submitting afterwards would leave an app admitted on the strength of a denial.
func TestOnboardingIsRefusedBeforeItIsRecorded(t *testing.T) {
	door := &denyingDoor{}
	admissions := estate.NewMemoryAdmissions()
	ctx := context.Background()

	err := estate.AdmitApp(ctx, door, admissions, gatekeeper(), admissionFor("canary-tokyo"))
	if err == nil {
		t.Fatal("the door denied this onboarding and it was accepted anyway")
	}
	if _, getErr := admissions.Get(ctx, "payments", "payments-checkout", "canary-tokyo"); !errors.Is(getErr, estate.ErrAdmissionNotFound) {
		t.Fatalf("a denied onboarding left a record behind: %v", getErr)
	}

	// The door must have been asked in its own vocabulary, with the environment as a PARAM —
	// an action name per environment would make this framework enumerate environments.
	if len(door.submitted) != 1 {
		t.Fatalf("the door must be asked exactly once, got %d", len(door.submitted))
	}
	intent := door.submitted[0]
	if intent.Action != "estate.admit" || intent.Params["env"] != "canary-tokyo" {
		t.Fatalf("the onboarding intent must carry the action and the opaque env: %+v", intent)
	}
	if intent.Params["reference"] != "CHG-TOKYO-4471" {
		t.Fatalf("the reference must reach the chain, got %v", intent.Params["reference"])
	}
	// The gatekeeper, not the record's own words about them: the door resolves roles from the
	// directory, so it needs the subject it was handed.
	if intent.Subject.ID != gatekeeper().ID {
		t.Fatalf("the door must rule on the gatekeeper, got %q", intent.Subject.ID)
	}
}

// The record names whoever the door ruled on, at our clock — never what the caller typed.
//
// Without this a caller can write somebody else's name into the onboarding record, or backdate
// it to before the ticket that authorised it, and the record is exactly what an auditor reads
// instead of asking us.
func TestTheRecordCannotBeAttributedToSomebodyElse(t *testing.T) {
	door := &allowingDoor{}
	admissions := estate.NewMemoryAdmissions()
	ctx := context.Background()

	claiming := admissionFor("canary-tokyo")
	claiming.DecidedBy = "platform-dana" // not the subject the door will rule on
	claiming.DecidedAt = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := estate.AdmitApp(ctx, door, admissions, gatekeeper(), claiming); err != nil {
		t.Fatalf(litAdmitting, err)
	}

	record, err := admissions.Get(ctx, "payments", "payments-checkout", "canary-tokyo")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if record.DecidedBy != gatekeeper().ID {
		t.Fatalf("the record must name the subject the door ruled on, got %q", record.DecidedBy)
	}
	if record.DecidedAt.Year() == 2019 {
		t.Fatal("a caller backdated an onboarding to before the ticket that authorised it")
	}
}

// Fixtures. The environment name is deliberately one no framework would enumerate.

func admissionFor(env string) estate.Admission {
	return estate.Admission{
		Team: "payments", App: "payments-checkout", Env: env,
		State:     estate.AdmissionAdmitted,
		Reason:    "onboarded for the Tokyo canary alongside the regional ledger",
		Reference: "CHG-TOKYO-4471",
		DecidedBy: "platform-dana", DecidedAt: time.Now().UTC(),
	}
}

func revocationFor(env string) estate.Admission {
	record := admissionFor(env)
	record.State = estate.AdmissionRevoked
	record.Reason = "the ledger migration finished, so this app no longer belongs here"
	record.Reference = "CHG-TOKYO-9902"
	return record
}

// deployer is the one service account every project deploys through — the identity this control
// exists because it cannot distinguish.
func deployer() mantlekeep.Subject { return mantlekeep.Subject{ID: "svc-ci-deploy"} }

// gatekeeper is a person, and deliberately NOT the deployer.
func gatekeeper() mantlekeep.Subject { return mantlekeep.Subject{ID: "platform-erin"} }

func floorForOpaqueEnv() estate.Floor {
	floor := estate.DefaultFloor()
	// The deployment names its own environments. This one is not in DefaultEnvTiers and never
	// will be, which is the point: MinTierFor reads config first and refuses an env nobody
	// ruled on.
	floor.EnvTiers = map[string]estate.Tier{"canary-tokyo": estate.TierDev}
	return floor
}

func placerForOpaqueEnv() *estate.Placer {
	return estate.NewPlacer([]estate.Cluster{
		{Name: "tokyo-app-1", Provider: "on-prem", Region: "jp-east", Env: "canary-tokyo",
			Purpose: "app", Residency: "jp", Reachable: true},
	}).WithCapacity([]estate.Capacity{{Cluster: "tokyo-app-1", AllocatablePct: 0.90}})
}

func manifestForOpaqueEnv(t *testing.T) estate.Manifest {
	t.Helper()
	manifest, err := estate.ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"checkout","runtime":"enterprise","image":"harbor/payments/checkout",
	             "placement":{"env":"canary-tokyo","purpose":"app","residency":"jp"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return manifest
}

// onlyRefusal returns the one sentence this outcome refused with.
//
// It reads Failed as well as Refused, and that is the seam's honest limit rather than sloppiness:
// the admission check hangs off [estate.ChangeTransformer], whose errors the manager reports as
// Failed because Refused carries the DOOR's words and the door was never asked. NOTES.md carries
// the wiring that makes this a Refused.
func onlyRefusal(t *testing.T, outcome estate.ApplyOutcome) string {
	t.Helper()
	switch {
	case len(outcome.Refused) == 1:
		return outcome.Refused[0].Refused
	case len(outcome.Failed) == 1:
		return outcome.Failed[0].Failed
	}
	t.Fatalf("expected exactly one stopped change, got refused=%+v failed=%+v",
		outcome.Refused, outcome.Failed)
	return ""
}

// allowingDoor mints a token for anything, so a test can prove admission stopped a change
// BEFORE the door rather than because the door refused it.
type allowingDoor struct {
	submitted []mantlekeep.Intent
}

func (d *allowingDoor) Submit(_ context.Context,
	intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {

	d.submitted = append(d.submitted, intent)
	return mantlekeep.ExecutionToken{Value: "tok-" + intent.ID, IntentID: intent.ID,
		ExpiresAt: time.Now().Add(time.Minute)}, nil
}

// denyingDoor refuses everything, in the door's own words.
type denyingDoor struct {
	submitted []mantlekeep.Intent
}

func (d *denyingDoor) Submit(_ context.Context,
	intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {

	d.submitted = append(d.submitted, intent)
	return mantlekeep.ExecutionToken{}, errors.New(
		"deny: onboarding to this environment is not yours to decide")
}

// recordingAppPort remembers which changes reached the adapter, because "the adapter was never
// called" is the property an admission refusal is actually claiming.
type recordingAppPort struct {
	applied map[string]string
}

func (p *recordingAppPort) Asset() string { return "app" }

func (p *recordingAppPort) Observe(context.Context, string) (estate.Observed, error) {
	return estate.Observed{}, nil
}

func (p *recordingAppPort) Apply(_ context.Context, token mantlekeep.ExecutionToken,
	change estate.DesiredItem) error {

	if p.applied == nil {
		p.applied = map[string]string{}
	}
	p.applied[change.Name] = token.IntentID
	return nil
}

// unreachableAdmissions is a store that cannot answer — the outage case, which must refuse.
type unreachableAdmissions struct{}

func (unreachableAdmissions) Get(context.Context, string, string, string) (estate.Admission, error) {
	return estate.Admission{}, errors.New(litStoreDown)
}

func (unreachableAdmissions) Admit(context.Context, estate.Admission) error {
	return errors.New(litStoreDown)
}

func (unreachableAdmissions) Revoke(context.Context, estate.Admission) error {
	return errors.New(litStoreDown)
}

func (unreachableAdmissions) AdmittedIn(context.Context, string) ([]estate.Admission, error) {
	return nil, errors.New(litStoreDown)
}
