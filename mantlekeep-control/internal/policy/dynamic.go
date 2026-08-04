package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync/atomic"
	"time"

	mantlekeep "mantlekeep.dev/control"
)

// This file makes the LAYERED CONFIG DYNAMIC — hot-reload with NO restart.
//
// WHY: today policy is resolved once at boot and the *Resolved is frozen into the
// engine. Changing a promote gate or sealing a new action means editing a file and
// RESTARTING the door — a change window, a redeploy, and a gap where governance is
// briefly the old rules. In a host that is unacceptable.
//
// The mechanism reuses resolve.go's Resolve() and its sealed cascade UNCHANGED. We
// only change WHEN it runs and WHERE the active snapshot lives:
//
//   1. LiveResolver holds the active *Resolved behind an atomic.Pointer. The engine
//      reads it per-request (one atomic load, no lock on the hot path).
//   2. A Watcher polls a Source (a file/dir or a Store table) for the current layers,
//      rebuilds a BRAND-NEW *Resolved via Resolve(...), and atomically SWAPS the
//      pointer. In-flight requests finish against the old snapshot; the next request
//      sees the new one — no request is ever evaluated against a half-applied config.
//   3. Because every reload runs Resolve() again, the SEALED FLOOR is enforced on
//      EVERY reload: a team that Puts a looser layer mid-flight is neutralised by the
//      same apply() seal rule that guards a boot-time file. The floor is now a LIVE
//      control the risk team can tighten in response to an incident, not a build-time
//      artifact.

// LiveResolver holds the active resolved cascade behind an atomic pointer so it can
// be swapped under running traffic with zero locks on the read path. It satisfies
// ActionAuthorizer (RequiredRole), so the RBAC engine reads the action→role authorization
// from the current snapshot on every evaluation (see rbac.go's WithLive).
type LiveResolver struct {
	cur atomic.Pointer[Resolved]
}

// NewLiveResolver seeds the live pointer with the boot-time resolved cascade.
func NewLiveResolver(initial *Resolved) *LiveResolver {
	l := &LiveResolver{}
	l.cur.Store(initial)
	return l
}

// Current returns the live snapshot (a single atomic load). Callers must treat it as
// read-only — a reload replaces the pointer, it never mutates a shared map in place.
func (l *LiveResolver) Current() *Resolved { return l.cur.Load() }

// Swap atomically installs a new resolved cascade. Called by the Watcher after a
// rebuild; the very next RequiredRole read sees it, no restart.
func (l *LiveResolver) Swap(res *Resolved) { l.cur.Store(res) }

// RequiredRole implements ActionAuthorizer against the LIVE snapshot — so a hot-reload
// that changes an action's required role takes effect on the next call.
func (l *LiveResolver) RequiredRole(action string) (mantlekeep.Role, bool) {
	return l.cur.Load().RequiredRole(action)
}

// Source abstracts WHERE the current layers come from: a file/dir today (kept for dev),
// a Store table for HA (Postgres/bbolt) later. The resolver is unchanged — it still
// just consumes []Layer. This is the same driver-swap pattern as the Store port.
type Source interface {
	Load(ctx context.Context) ([]Layer, error)
}

// SourceFunc adapts a plain function to a Source.
type SourceFunc func(ctx context.Context) ([]Layer, error)

// Load implements Source.
func (f SourceFunc) Load(ctx context.Context) ([]Layer, error) { return f(ctx) }

// Watcher polls a Source and, on a REAL change, rebuilds the cascade and atomically
// swaps it into a LiveResolver. Poll is the honest floor — it works everywhere with
// zero infra; a change is live in <= interval, not instantly. (LISTEN/NOTIFY and NATS
// pub/sub are sub-second alternatives behind the same shape, per the design spec, and
// live in adapter modules so the core links no transport.)
type Watcher struct {
	src      Source
	live     *LiveResolver
	fallback ActionAuthorizer // the product registry, re-attached to every rebuilt *Resolved
	ladder   RoleLadder       // the deployment's role vocabulary, passed to every rebuilt Resolve
	lastHash string           // content hash of the last-applied layers — dedup so a flapping
	// or unchanged source does not thrash the pointer every poll.
	onReload func(revision string, res *Resolved) // optional hook (audit/log) fired on a real swap
}

// NewWatcher wires a watcher over an already-built LiveResolver. initial is the layer
// set the caller used to build the seeded snapshot; hashing it here means the FIRST
// poll of an unchanged source is a no-op (no needless re-swap on boot). ladder is the
// deployment's role vocabulary — every rebuilt cascade resolves against the SAME ladder as
// the seeded one, so a renamed-role deployment's seal checks stay consistent across reloads.
func NewWatcher(src Source, live *LiveResolver, fallback ActionAuthorizer, ladder RoleLadder, initial []Layer) *Watcher {
	return &Watcher{
		src:      src,
		live:     live,
		fallback: fallback,
		ladder:   ladder,
		lastHash: hashLayers(initial),
	}
}

// OnReload registers a callback fired after a real swap (e.g. write a "policy.reload"
// audit record, or log the new revision). Returns the receiver to chain.
func (w *Watcher) OnReload(fn func(revision string, res *Resolved)) *Watcher {
	w.onReload = fn
	return w
}

// Reload runs ONE poll iteration: load the current layers, dedup by content hash, and
// on a real change rebuild the cascade (running the sealed floor again) and atomically
// swap it in. Returns whether a swap happened. It is safe to call directly — the test
// drives it deterministically instead of waiting on a ticker.
func (w *Watcher) Reload(ctx context.Context) (changed bool, err error) {
	layers, err := w.src.Load(ctx)
	if err != nil {
		// A transient source error must NOT tear down the live policy — keep serving
		// the last-good snapshot (fail static, not open). The next poll retries.
		return false, err
	}
	h := hashLayers(layers)
	if h == w.lastHash {
		return false, nil // no real change — do not thrash the pointer
	}
	// Rebuild from scratch: Resolve() re-runs the sealed cascade, so a team layer that
	// tries to loosen a sealed floor is rejected on THIS reload exactly as at boot.
	res := Resolve(w.ladder, layers...)
	if w.fallback != nil {
		res.WithFallback(w.fallback) // re-attach the product registry (product layer)
	}
	w.live.Swap(res) // the atomic swap — next request sees the new policy, no restart
	w.lastHash = h
	if w.onReload != nil {
		w.onReload(h, res)
	}
	return true, nil
}

// Start launches the poll loop in a goroutine, ticking every interval until ctx is
// cancelled. For a long-running server ctx is context.Background() (never cancelled);
// on shutdown a cancellable ctx stops the goroutine cleanly.
func (w *Watcher) Start(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = w.Reload(ctx) // errors are swallowed: keep last-good, retry next tick
			}
		}
	}()
}

// hashLayers produces a stable content hash of the layer set for change-detection.
// Go's encoding/json emits map keys in sorted order, so the same layers always hash
// the same — a Put/write with identical content does not trigger a reload.
func hashLayers(layers []Layer) string {
	b, err := json.Marshal(layers)
	if err != nil {
		// Marshal of these plain string maps cannot realistically fail; if it ever did,
		// return a sentinel that differs from any real hash so we err toward reloading.
		return "unhashable"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
