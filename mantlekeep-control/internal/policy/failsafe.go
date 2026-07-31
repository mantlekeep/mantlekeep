package policy

import (
	"context"
	"strings"
	"sync/atomic"

	mantlekeep "mantlekeep.dev/control"
)

// Failsafe wraps a primary PolicyEvaluator with a resilience fallback. While the
// primary is healthy its decisions pass through unchanged. But if the primary
// ERRORS (the policy engine or its data source is unavailable) — or if failsafe
// is deliberately TRIPPED — the door degrades to a cached, compiled-in read-only
// policy: read actions are allowed, every write is denied. The system fails
// SAFE, never open and never fully dead. Reads keep working (dashboards, logs)
// while no state-changing action can slip through on a degraded policy.
//
// "Cached" here means the fallback policy needs no engine and no network: it is
// the static ruleset below, always in memory, so it is available exactly when
// the primary is not.
type Failsafe struct {
	primary mantlekeep.PolicyEvaluator
	tripped atomic.Bool
}

// NewFailsafe wraps a primary evaluator with the read-only fallback.
func NewFailsafe(primary mantlekeep.PolicyEvaluator) *Failsafe {
	return &Failsafe{primary: primary}
}

// Trip forces failsafe (read-only) mode on — e.g. an operator suspends writes,
// or a health check detects a bad policy source.
func (f *Failsafe) Trip() { f.tripped.Store(true) }

// Reset returns to normal operation (primary policy back in charge).
func (f *Failsafe) Reset() { f.tripped.Store(false) }

// Tripped reports whether failsafe read-only mode is currently active.
func (f *Failsafe) Tripped() bool { return f.tripped.Load() }

// Evaluate implements mantlekeep.PolicyEvaluator.
func (f *Failsafe) Evaluate(ctx context.Context, input mantlekeep.PolicyInput) (mantlekeep.Decision, error) {
	if f.tripped.Load() {
		return f.readOnly(input), nil
	}
	d, err := f.primary.Evaluate(ctx, input)
	if err != nil {
		// Primary engine failure → degrade safe rather than propagate an error
		// that would either block everything or tempt a fail-open shortcut.
		return f.readOnly(input), nil
	}
	return d, nil
}

// readOnly is the cached fallback policy: allow reads, deny writes.
func (f *Failsafe) readOnly(input mantlekeep.PolicyInput) mantlekeep.Decision {
	if isRead(input.Intent.Action) {
		return mantlekeep.Decision{
			Action:   mantlekeep.ActionAllow,
			Reason:   "failsafe: read-only mode — read permitted",
			PolicyID: policyID("failsafe"),
			Warnings: []string{"policy degraded to cached read-only mode"},
		}
	}
	return mantlekeep.Decision{
		Action:   mantlekeep.ActionDeny,
		Reason:   "failsafe: read-only mode — writes are suspended",
		PolicyID: policyID("failsafe"),
		Warnings: []string{"policy degraded to cached read-only mode"},
	}
}

// isRead reports whether an action only observes state (safe under failsafe).
func isRead(action string) bool {
	if action == "" {
		return false // unknown → treat as a write, deny
	}
	for _, suffix := range []string{".read", ".view", ".list", ".status", ".get"} {
		if strings.HasSuffix(action, suffix) {
			return true
		}
	}
	return false
}
