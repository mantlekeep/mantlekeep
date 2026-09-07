package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
)

// The watcher itself: a goroutine, a ticker, and the decision to start one at all. The reload
// tests drive Reload synchronously, which proves the SWAP; this proves the door actually polls,
// so a real deployment picks up a file edit with nobody calling anything.

// TestTheDoorPicksUpAPolicyEditWhileItIsRunning edits the register on disk after boot and waits
// for a decision to change. Nothing calls reload — the running door has to notice.
func TestTheDoorPicksUpAPolicyEditWhileItIsRunning(t *testing.T) {
	const action = "watch.deploy-registered-app"
	policyDir := t.TempDir()
	document := filepath.Join(policyDir, "register.json")

	write := func(apps string) {
		t.Helper()
		body := `{"role_actions":{"L3-Consumer":["` + action + `"]},` +
			`"floors":{"` + action + `":[{"kind":"allowlist","param":"app","values":[` + apps + `],` +
			`"message":"application is not on the onboarding register"}]}}`
		if err := os.WriteFile(document, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`"already-onboarded"`)
	t.Setenv(grants.PolicyDirEnv, policyDir)
	t.Setenv("MANTLEKEEP_POLICY_RELOAD", "1") // poll every second, so the test is seconds not minutes

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if started := startGrantsWatcher(ctx); !started {
		t.Fatal("no watcher started even though a policy directory is configured")
	}
	t.Cleanup(func() {
		stop()
		// Give the goroutine a tick to observe cancellation before other tests run.
		time.Sleep(50 * time.Millisecond)
	})

	// Seed from the configured source so this test's register is what is in force. (Reading
	// the engine at all triggers the lazy first load, which already sees this env — so the
	// seed is frequently a no-op, and asserting that it CHANGED anything would be asserting
	// the order of two loads rather than the behaviour under test.)
	if _, _, err := policy.ReloadGrants(ctx, grants.EnvSource{}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	// Asked through the PUBLIC evaluator port — the same call the door makes — so this test
	// cannot pass by reaching into engine internals the real request path never touches.
	engine := policy.NewRBAC()
	allowed := func() bool {
		decision, err := engine.Evaluate(ctx, mantlekeep.PolicyInput{
			Subject: mantlekeep.PolicySubject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
			Intent: mantlekeep.PolicyIntent{
				Action: action,
				Goal:   "deploy newcomer",
				Params: map[string]any{"app": "newcomer"},
			},
		})
		return err == nil && decision.Action == mantlekeep.ActionAllow
	}
	if allowed() {
		t.Fatal("an unregistered application was admitted before it was ever registered")
	}

	// The edit. Nobody tells the door.
	write(`"already-onboarded","newcomer"`)

	deadline := time.Now().Add(15 * time.Second)
	for !allowed() {
		if time.Now().After(deadline) {
			t.Fatalf("the running door never picked up the edit — a platform team adding an "+
				"application to the register would still be waiting for a redeploy (revision "+
				"still %s)", policy.RevisionInForce())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A binary with no policy environment reads the embedded defaults, which cannot change while it
// runs. Polling them would be a goroutine whose only job is to discover nothing, every second,
// for the life of the process.
func TestNoWatcherStartsWhenNoPolicySourceIsConfigured(t *testing.T) {
	for _, env := range []string{
		grants.PolicyDirEnv, grants.PlatformPolicyEnv, grants.EnvOverride, grants.FloorsEnvOverride,
	} {
		t.Setenv(env, "")
	}
	if startGrantsWatcher(context.Background()) {
		t.Fatal("a watcher started with no configured source — it can only ever poll the " +
			"binary's own embedded documents")
	}
}
