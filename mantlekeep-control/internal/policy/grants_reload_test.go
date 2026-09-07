package policy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// These tests are about the property that makes hot-reload safe to run in production: a policy
// change takes effect with no restart, and a BROKEN policy change takes effect not at all.
//
// They drive Reload directly rather than waiting on a ticker. The ticker is time; the swap is
// the behaviour, and a test that sleeps to observe it is slower and less certain about both.

// documentSource is a Loader a test controls: it hands back whatever the test last set, so a
// case can express "the source now says X" and "the source is broken" without touching disk.
type documentSource struct {
	held   *grants.Grants
	floors *grants.Floors
	err    error
}

func (s *documentSource) Load(context.Context) (*grants.Grants, *grants.Floors, grants.Revision, error) {
	if s.err != nil {
		return nil, nil, "", s.err
	}
	return s.held, s.floors, grants.RevisionOfDocuments(s.held, s.floors), nil
}

func grantingDocument(role string, actions ...string) *grants.Grants {
	return &grants.Grants{RoleActions: map[string][]string{role: actions}}
}

func noFloors() *grants.Floors { return &grants.Floors{Floors: map[string][]grants.FloorRule{}} }

// isolatePolicy restores whatever snapshot was in force before a test replaced it, so these
// tests cannot leak a document into the rest of the package.
func isolatePolicy(t *testing.T) {
	t.Helper()
	previous := ensurePolicy()
	t.Cleanup(func() { livePolicy.Store(previous) })
}

// THE POINT OF THE WHOLE CHANGE: a grant that did not exist when the door started is honoured
// without restarting the door.
func TestANewGrantTakesEffectWithNoRestart(t *testing.T) {
	isolatePolicy(t)
	engine := NewRBAC()
	const action = "reload.onboard-a-new-app"

	source := &documentSource{held: grantingDocument("L2-Dev"), floors: noFloors()}
	if _, _, err := ReloadGrants(context.Background(), source); err != nil {
		t.Fatalf("seeding the starting policy: %v", err)
	}
	if engine.actionAllowed([]string{"L2-Dev"}, action, nil) {
		t.Fatalf("%q was allowed before it was ever granted — this test proves nothing", action)
	}

	source.held = grantingDocument("L2-Dev", action)
	revision, changed, err := ReloadGrants(context.Background(), source)
	if err != nil || !changed {
		t.Fatalf("reload of a valid change: changed=%v err=%v", changed, err)
	}
	if !engine.actionAllowed([]string{"L2-Dev"}, action, nil) {
		t.Fatalf("%q still denied after the document granting it was installed at revision %s — "+
			"the engine is deciding on documents it no longer holds", action, revision)
	}
}

// THE PROPERTY THAT MAKES IT SAFE: a source that fails is refused, and the last good policy is
// still the one deciding. A broken reload that emptied the grants would not look like an outage
// — it would look like a working deny-all, which is far harder to notice.
func TestABrokenSourceIsRefusedAndTheGoodPolicyKeepsDeciding(t *testing.T) {
	isolatePolicy(t)
	engine := NewRBAC()
	const action = "reload.keeps-deciding"

	source := &documentSource{held: grantingDocument("L2-Dev", action), floors: noFloors()}
	good, _, err := ReloadGrants(context.Background(), source)
	if err != nil {
		t.Fatalf("seeding the good policy: %v", err)
	}

	source.err = errors.New("policy documents are unreadable")
	inForce, changed, err := ReloadGrants(context.Background(), source)
	if err == nil {
		t.Fatal("a broken source reloaded successfully — nothing is validating the documents")
	}
	if changed {
		t.Fatal("a broken source reported a change; it must install nothing")
	}
	if inForce != good {
		t.Fatalf("refused reload reports revision %s in force, but %s is what is deciding — a "+
			"caller logging this would name the wrong policy", inForce, good)
	}
	if !engine.actionAllowed([]string{"L2-Dev"}, action, nil) {
		t.Fatalf("%q is denied after a REFUSED reload — the broken document was applied", action)
	}
}

// A floor is the app-onboarding register: an allowlist naming who may be deployed. Adding a
// name to it must not need a redeploy, or the register is a release artefact and onboarding
// waits on a change window.
func TestAnAllowlistFloorGainsAMemberWithNoRestart(t *testing.T) {
	isolatePolicy(t)
	const action = "reload.deploy-registered-app"

	register := func(apps ...string) *grants.Floors {
		return &grants.Floors{Floors: map[string][]grants.FloorRule{
			action: {{Kind: "allowlist", Param: "app", Values: apps, Message: "app is not on the register"}},
		}}
	}
	source := &documentSource{
		held:   grantingDocument("L2-Dev", action),
		floors: register("already-onboarded"),
	}
	if _, _, err := ReloadGrants(context.Background(), source); err != nil {
		t.Fatalf("seeding the register: %v", err)
	}

	newcomer := map[string]any{"app": "newcomer"}
	if denied, _ := admitFloor(DefaultRoleLadder(), action, newcomer, nil); !denied {
		t.Fatal("an unregistered app was admitted before it was ever registered")
	}

	source.floors = register("already-onboarded", "newcomer")
	if _, changed, err := ReloadGrants(context.Background(), source); err != nil || !changed {
		t.Fatalf("registering a new app: changed=%v err=%v", changed, err)
	}
	if denied, reason := admitFloor(DefaultRoleLadder(), action, newcomer, nil); denied {
		t.Fatalf("a registered app is still refused after the register was reloaded: %s", reason)
	}
}

// Reloading the same documents must not swap the pointer. Not an optimisation: OnReload is
// wired to an operator log and, later, to the audit chain, and a watcher that announced a
// governance change every poll would make the real one unfindable.
func TestReloadingUnchangedDocumentsIsNotAChange(t *testing.T) {
	isolatePolicy(t)
	source := &documentSource{held: grantingDocument("L2-Dev", "reload.same"), floors: noFloors()}
	if _, _, err := ReloadGrants(context.Background(), source); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, changed, err := ReloadGrants(context.Background(), source); err != nil || changed {
		t.Fatalf("re-reading identical documents reported changed=%v err=%v", changed, err)
	}
}

// The watcher's callbacks are how a refusal stops being silent, so they are part of the
// contract, not decoration.
func TestTheWatcherReportsWhatItInstalledAndWhatItRefused(t *testing.T) {
	isolatePolicy(t)
	source := &documentSource{held: grantingDocument("L2-Dev"), floors: noFloors()}

	var installed []grants.Revision
	var refusedInForce grants.Revision
	var refusedErr error
	watcher := NewGrantsWatcher(source).
		OnReload(func(revision grants.Revision) { installed = append(installed, revision) }).
		OnRefuse(func(inForce grants.Revision, err error) { refusedInForce, refusedErr = inForce, err })

	if _, err := watcher.Reload(context.Background()); err != nil {
		t.Fatalf("first reload: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("OnReload fired %d times for one real change", len(installed))
	}

	source.err = errors.New("source is down")
	if _, err := watcher.Reload(context.Background()); err == nil {
		t.Fatal("the watcher hid a source failure")
	}
	if refusedErr == nil {
		t.Fatal("OnRefuse did not fire — a refused reload would be silent")
	}
	if refusedInForce != installed[0] {
		t.Fatalf("OnRefuse named revision %s as in force; %s is", refusedInForce, installed[0])
	}
}

// The reload path runs the SAME merge and the SAME platform seal as boot. If it did not,
// hot-reload would be a second, laxer way into the policy — a product doc could install a
// grant at 10am that the same doc would have been refused for at boot.
func TestReloadEnforcesThePlatformSeal(t *testing.T) {
	isolatePolicy(t)
	dir := t.TempDir()

	platform := filepath.Join(dir, "platform.json")
	writeJSON(t, platform, `{"role_actions":{"L1-Platform":["seal.platform-only"]}}`)
	productDir := filepath.Join(dir, "products")
	if err := os.Mkdir(productDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// A product doc claiming an action the PLATFORM grants — the exact thing the seal exists
	// to refuse.
	writeJSON(t, filepath.Join(productDir, "product.json"),
		`{"role_actions":{"L2-Dev":["seal.platform-only"]}}`)

	t.Setenv(grants.PlatformPolicyEnv, platform)
	t.Setenv(grants.PolicyDirEnv, productDir)

	_, changed, err := ReloadGrants(context.Background(), grants.EnvSource{})
	if err == nil {
		t.Fatal("a product doc granting a sealed platform action was ACCEPTED on reload — the " +
			"seal is enforced at boot only, so hot-reload is a way around it")
	}
	if changed {
		t.Fatal("a sealed-action violation reported a change")
	}
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The atomic pointer exists so a reload can land while requests are being decided. That claim
// is only worth making if it is checked under -race: a reader holding a map another goroutine
// is rebuilding is the exact bug the snapshot design is meant to make impossible.
func TestDecisionsAndReloadsRunTogetherSafely(t *testing.T) {
	isolatePolicy(t)
	engine := NewRBAC()
	const action = "reload.under-load"
	source := &documentSource{held: grantingDocument("L2-Dev", action), floors: noFloors()}
	if _, _, err := ReloadGrants(context.Background(), source); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// Two documents that both grant the action, so every decision taken mid-swap has the same
	// right answer whichever snapshot it landed on. A flapping ANSWER would be a policy bug;
	// this test is looking for a memory bug.
	documents := []*grants.Grants{
		grantingDocument("L2-Dev", action),
		grantingDocument("L2-Dev", action, "reload.under-load-extra"),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			swapping := &documentSource{held: documents[i%2], floors: noFloors()}
			if _, _, err := ReloadGrants(context.Background(), swapping); err != nil {
				t.Errorf("reload %d: %v", i, err)
				return
			}
		}
	}()
	for i := 0; i < 200; i++ {
		if !engine.actionAllowed([]string{"L2-Dev"}, action, nil) {
			t.Fatalf("decision %d denied an action granted by BOTH documents — a request saw "+
				"neither snapshot whole", i)
		}
	}
	<-done
}
