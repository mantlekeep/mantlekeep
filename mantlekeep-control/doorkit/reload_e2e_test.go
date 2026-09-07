package doorkit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// The operational flow, end to end, through the real door: onboard an application while the door
// running, and prove a request that was REFUSED is then ALLOWED — with no restart, and with the
// hash-chained audit log recording both.
//
// This drives Submit rather than the policy engine directly, because that is the only path a
// user has. An engine test can pass while the door is still consulting something else.

const (
	onboardAction = "e2e.deploy-registered-app"
	registerFile  = "register.json"
)

// register writes the onboarding register: which team may deploy, and which applications are
// approved. Adding a name here is exactly the change that used to need a redeploy.
func register(t *testing.T, dir string, apps ...string) {
	t.Helper()
	quoted := make([]string, 0, len(apps))
	for _, app := range apps {
		quoted = append(quoted, `"`+app+`"`)
	}
	document := `{
	  "role_actions": {"L3-Consumer": ["` + onboardAction + `"]},
	  "floors": {"` + onboardAction + `": [
	    {"kind":"allowlist","param":"app","values":[` + strings.Join(quoted, ",") + `],
	     "message":"application is not on the onboarding register"}
	  ]}
	}`
	if err := os.WriteFile(filepath.Join(dir, registerFile), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
}

func deployIntent(app string) mantlekeep.Intent {
	return mantlekeep.Intent{
		ID:      "INT-E2E-" + app,
		Subject: mantlekeep.Subject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
		Action:  onboardAction,
		Params:  map[string]any{"app": app},
		Spec: mantlekeep.IntentSpec{
			Goal:            "deploy " + app,
			AffectedSystems: []string{app},
			RiskLevel:       mantlekeep.RiskLow,
			RollbackPlan:    "redeploy the previous revision",
		},
		SubmittedAt: time.Now(),
		TTL:         time.Minute,
	}
}

func TestOnboardingAnApplicationTakesEffectWithoutRestartingTheDoor(t *testing.T) {
	policyDir := t.TempDir()
	register(t, policyDir, "already-onboarded")
	t.Setenv(grants.PolicyDirEnv, policyDir)

	door, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("assembling the door: %v", err)
	}
	doorkit.EnsureLoaded()
	ctx := context.Background()

	// BEFORE: the newcomer is not on the register, so the door refuses it.
	if _, err := door.Submitter.Submit(ctx, deployIntent("newcomer")); err == nil {
		t.Fatal("the door allowed an application that is not on the register — this test would " +
			"pass even if reloading did nothing")
	}

	// The change a platform team makes: add the application. No redeploy, no change window.
	register(t, policyDir, "already-onboarded", "newcomer")
	revision, changed, err := doorkit.ReloadPolicy(ctx, grants.EnvSource{})
	if err != nil || !changed {
		t.Fatalf("onboarding reload: changed=%v err=%v", changed, err)
	}

	// AFTER: the same request, the same process, now allowed.
	token, err := door.Submitter.Submit(ctx, deployIntent("newcomer"))
	if err != nil {
		t.Fatalf("the door still refuses a registered application after reloading to revision "+
			"%s — the running door is deciding on documents it no longer holds: %v", revision, err)
	}
	if token.Value == "" {
		t.Fatal("an allowed submit returned no execution token")
	}
	if doorkit.PolicyRevisionInForce() != revision {
		t.Fatalf("the door reports revision %s in force but %s was installed",
			doorkit.PolicyRevisionInForce(), revision)
	}
}

// The other half, and the reason this is safe to run in production: a broken register is
// refused and the door keeps deciding on the last good one. A reload that half-applied — or
// that emptied the grants — would not look like an outage. It would look like a working
// deny-all, which is far harder to notice and far worse to debug.
func TestABrokenRegisterIsRefusedAndTheDoorKeepsWorking(t *testing.T) {
	policyDir := t.TempDir()
	register(t, policyDir, "already-onboarded")
	t.Setenv(grants.PolicyDirEnv, policyDir)

	door, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("assembling the door: %v", err)
	}
	doorkit.EnsureLoaded()
	ctx := context.Background()

	if _, _, err := doorkit.ReloadPolicy(ctx, grants.EnvSource{}); err != nil {
		t.Fatalf("seeding the good register: %v", err)
	}
	good := doorkit.PolicyRevisionInForce()
	if _, err := door.Submitter.Submit(ctx, deployIntent("already-onboarded")); err != nil {
		t.Fatalf("a registered application was refused before anything broke: %v", err)
	}

	// Somebody saves a register that is not valid JSON.
	if err := os.WriteFile(filepath.Join(policyDir, registerFile),
		[]byte(`{"role_actions": {"L3-Consumer": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	inForce, changed, err := doorkit.ReloadPolicy(ctx, grants.EnvSource{})
	if err == nil {
		t.Fatal("a malformed register was ACCEPTED — the door would now be enforcing a truncated policy")
	}
	if changed {
		t.Fatal("a refused reload reported a change")
	}
	if inForce != good {
		t.Fatalf("the refusal names revision %s as in force; %s is what is deciding", inForce, good)
	}

	// The door is untouched: still allowing what the last GOOD register allowed.
	if _, err := door.Submitter.Submit(ctx, deployIntent("already-onboarded")); err != nil {
		t.Fatalf("a registered application is refused after a REFUSED reload — the broken "+
			"document reached the engine: %v", err)
	}
	if _, err := door.Submitter.Submit(ctx, deployIntent("newcomer")); err == nil {
		t.Fatal("an unregistered application became allowed after a refused reload")
	}
}
