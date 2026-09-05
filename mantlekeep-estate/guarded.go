package estate

import (
	"context"
	"fmt"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Approved is the half of an adapter that is actually about its backend.
//
// It is reached ONLY after every guard in [Guarded] has passed, so an implementation may
// assume what those guards establish: a live token, a change this adapter owns, and floor
// limits that were resolved by the engine rather than invented locally.
type Approved interface {
	// Asset names the kind of thing this adapter provisions ("app", "postgres", "kafka").
	Asset() string

	// Kinds lists the change kinds it handles within that asset. Empty means all of them.
	Kinds() []string

	// Observe reports what is really there. Guarded does not wrap it: observation has no
	// token to check and no change to route, so there is nothing to guard.
	Observe(ctx context.Context, team string) (Observed, error)

	// ApplyApproved makes the change. Everything a guard would have refused has already
	// been refused.
	ApplyApproved(ctx context.Context, token mantlekeep.ExecutionToken, change DesiredItem) error
}

// Guarded turns an [Approved] into a [Port], running the refusals every adapter owes
// before its backend is touched.
//
// # Why this exists
//
// Four guards were written independently in every adapter, in the same order, with the
// same meaning and slightly different words. Each was correct. That is the problem: four
// correct copies is four places to forget the fifth, and the guards are not convenience
// code — they are what makes "govern before execute" true at the edge. An adapter that
// skips the token check executes a decision nobody made.
//
// Documenting the rule was not enough, because a document cannot fail a build. Here the
// adapter does not implement Apply at all; it implements ApplyApproved, which is only
// reachable through the guards. Skipping them is not a mistake an author can make — there
// is no code path that arrives without them.
//
// Wrapping is deliberate rather than embedding: an embedded base can be shadowed by a
// method on the outer type, which would silently reinstate the very hole this closes.
//
// The floor-limit guard stays with the adapter. Only the adapter knows which concrete
// limits type its backend needs, and a check here could assert nothing more specific than
// "not nil" — which would pass for the wrong type and defeat the point.
func Guarded(approved Approved) Port {
	return guarded{approved: approved, kinds: kindSet(approved.Kinds())}
}

type guarded struct {
	approved Approved
	kinds    map[string]bool // empty means every kind of this asset
}

func (g guarded) Asset() string { return g.approved.Asset() }

func (g guarded) Observe(ctx context.Context, team string) (Observed, error) {
	return g.approved.Observe(ctx, team)
}

// Apply runs the guards, then delegates. The order is the argument: identity of the
// decision first, then whether this adapter is the right one to carry it out.
func (g guarded) Apply(ctx context.Context, token mantlekeep.ExecutionToken,
	change DesiredItem) error {

	asset := g.approved.Asset()

	// No token means no decision behind this. The door decides, the adapter executes.
	if token.Value == "" {
		return fmt.Errorf("%s: refusing to apply %q with no execution token — the door "+
			"decides, the adapter executes", asset, change.Name)
	}

	// An expired capability is not a decision either. The door's answer had a lifetime,
	// and applying after it has run out is applying an answer nobody would give now —
	// which is exactly the window a replayed token would use.
	if !token.Valid(time.Now()) {
		return fmt.Errorf("%s: refusing to apply %q — the execution token for intent %q "+
			"expired at %s", asset, change.Name, token.IntentID, token.ExpiresAt)
	}

	// A change for somebody else is refused rather than attempted. An adapter that tried
	// anyway would fail somewhere deeper, in its backend's words rather than its own.
	if change.Asset != asset {
		return fmt.Errorf("%s: %s/%s is not this adapter's concern",
			asset, change.Asset, change.Kind)
	}
	if len(g.kinds) > 0 && !g.kinds[change.Kind] {
		return fmt.Errorf("%s: %s/%s is not this adapter's concern",
			asset, change.Asset, change.Kind)
	}

	return g.approved.ApplyApproved(ctx, token, change)
}

func kindSet(kinds []string) map[string]bool {
	set := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		set[kind] = true
	}
	return set
}
