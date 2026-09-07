package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// This file is the WRITE side of the live policy snapshot: how new documents become the law,
// and — more importantly — how bad ones do not.
//
// The shape is dynamic.go's, deliberately: load, compare by revision, rebuild, swap. What is
// specific here is the ORDER. The whole snapshot is built before anything is installed, so a
// document that fails to load, fails to merge, or violates the platform seal fails while the
// PREVIOUS snapshot is still the one deciding. There is no window in which the door is running
// on a partially applied policy, and no failure mode in which a broken file empties the grants
// — which would not look like an outage, it would look like a working deny-all.

// ReloadGrants re-reads the policy documents through source and installs them if they are
// valid.
//
// Returns the revision now in force and whether it changed. On error NOTHING is installed and
// the revision returned is the one still deciding — so a caller logging the result reports what
// is actually enforcing, not what it hoped to enforce.
func ReloadGrants(ctx context.Context, source grants.Loader) (inForce grants.Revision, changed bool, err error) {
	current := ensurePolicy() // also guarantees there IS a good snapshot to fall back to

	held, floors, revision, err := source.Load(ctx)
	if err != nil {
		return current.revision, false, fmt.Errorf("policy reload: %w", err)
	}
	if revision == current.revision {
		return current.revision, false, nil // same documents — do not thrash the pointer
	}
	snapshot, err := buildSnapshot(held, floors)
	if err != nil {
		return current.revision, false, fmt.Errorf("policy reload: %w", err)
	}
	// The swap. Built first, installed last: every way this could have failed has already
	// happened above, with the old snapshot still serving.
	livePolicy.Store(snapshot)
	return snapshot.revision, true, nil
}

// RevisionInForce is the revision of the DOCUMENTS the engine accepted — the value the source
// reported when these documents were installed.
//
// The engine's own answer, not the file's: a surface that re-read the source would show what is
// on disk while the door kept enforcing the last documents it ACCEPTED, and would show it
// confidently. It is what the reload log names, so an operator reading "still enforcing X" can
// match X against the source that produced it.
//
// A surface that also reports the ENGINE's built-in grants (the L0-SuperAdmin wildcard, which is
// code and appears in no document) must not re-derive a revision from that view: the view is not
// a document, and hashing it would report a revision no document-reading loader can reproduce.
func RevisionInForce() grants.Revision { return ensurePolicy().revision }

// GrantsWatcher polls a [grants.Loader] and installs each valid change.
//
// Polling is the honest floor: it needs no infrastructure and works in an air-gapped
// deployment, at the cost of a change being live within one interval rather than instantly.
// A push source (LISTEN/NOTIFY, a bundle server) is the same Loader behind the same swap, and
// lives in an adapter so the core links no transport.
type GrantsWatcher struct {
	source   grants.Loader
	onReload func(revision grants.Revision)
	onRefuse func(inForce grants.Revision, err error)
}

// NewGrantsWatcher wires a watcher over the source the door loads policy from.
func NewGrantsWatcher(source grants.Loader) *GrantsWatcher {
	return &GrantsWatcher{source: source}
}

// OnReload registers a callback fired after a real swap, carrying the revision now in force.
// Reloading policy is itself an event worth recording: a decision reviewed later is only
// explainable if the trail says which policy was in force when it was made.
func (w *GrantsWatcher) OnReload(fn func(revision grants.Revision)) *GrantsWatcher {
	w.onReload = fn
	return w
}

// OnRefuse registers a callback fired when a reload was REFUSED, carrying the revision still in
// force and why the new documents were rejected.
//
// It exists because a refused reload is silent by design — the door keeps working — and silence
// is exactly how a deployment ends up running month-old policy while someone believes they
// changed it. The callback is what turns "still fine" into "still fine, and here is what you
// tried to install".
func (w *GrantsWatcher) OnRefuse(fn func(inForce grants.Revision, err error)) *GrantsWatcher {
	w.onRefuse = fn
	return w
}

// Reload runs ONE poll iteration. Exported so a caller can drive it directly — on a signal, on
// an admin request, or deterministically in a test instead of waiting on a ticker.
func (w *GrantsWatcher) Reload(ctx context.Context) (changed bool, err error) {
	inForce, changed, err := ReloadGrants(ctx, w.source)
	switch {
	case err != nil:
		if w.onRefuse != nil {
			w.onRefuse(inForce, err)
		}
		return false, err
	case changed && w.onReload != nil:
		w.onReload(inForce)
	}
	return changed, nil
}

// Start launches the poll loop until ctx is cancelled.
func (w *GrantsWatcher) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// The error is handled by OnRefuse and must not stop the loop: a source that
				// is briefly unreadable is a reason to keep the last good policy and retry,
				// not a reason to stop noticing changes for the life of the process.
				_, _ = w.Reload(ctx)
			}
		}
	}()
}
