package app

import (
	"context"
	"fmt"
	"os"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
)

// This file wires the GRANT and FLOOR documents to the same hot-reload treatment door.go
// already gives the layered cascade.
//
// Until now those documents were the one part of governance that still needed a restart to
// change. That is the part a regulated deployment feels: onboarding an application — naming it
// in an allowlist floor, granting its team an action — is routine and frequent, and it was
// landing on the release cadence. So it arrived through a change request, or it arrived through
// someone turning the guardrail off. Both are worse than reloading a file.

// startGrantsWatcher polls the configured policy documents and installs each valid change,
// returning whether a watcher was started.
//
// It starts only when a source can actually change. A binary with no policy environment reads
// the embedded defaults, which cannot change while it runs, so polling them would be a
// goroutine that exists to discover nothing.
func startGrantsWatcher(ctx context.Context) bool {
	if !policySourceIsConfigured() {
		return false
	}
	interval := reloadInterval()
	policy.NewGrantsWatcher(grants.EnvSource{}).
		OnReload(func(revision grants.Revision) {
			// A change to who may do what is itself an event worth recording: a decision
			// reviewed months later is only explainable if the trail says which policy was in
			// force when it was made. The audit chain record is written where the AuditLogger
			// is in scope; this is the operator-visible half.
			fmt.Printf("policy: hot-reloaded grants + floors — no restart (revision %s)\n", revision)
		}).
		OnRefuse(func(inForce grants.Revision, err error) {
			// Loud on purpose. A refused reload is invisible from the outside — the door keeps
			// answering, correctly, on the last good documents — and that silence is exactly
			// how a deployment ends up enforcing month-old policy while someone believes they
			// changed it. Say what was refused AND what is still deciding.
			fmt.Printf("policy: REFUSED new grants + floors, still enforcing revision %s: %v\n", inForce, err)
		}).
		Start(ctx, interval)
	fmt.Printf("policy: grants + floors hot-reload watcher started (poll every %s)\n", interval)
	return true
}

// policySourceIsConfigured reports whether any policy document comes from outside the binary.
func policySourceIsConfigured() bool {
	for _, env := range []string{
		grants.PolicyDirEnv,
		grants.PlatformPolicyEnv,
		grants.EnvOverride,
		grants.FloorsEnvOverride,
	} {
		if os.Getenv(env) != "" {
			return true
		}
	}
	return false
}
