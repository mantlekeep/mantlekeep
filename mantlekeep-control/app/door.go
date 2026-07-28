package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/internal/audit"
	"mantlekeep.dev/control/internal/policy"
	"mantlekeep.dev/control/internal/sdk"
)

// BuildDoor assembles the one door (identity + policy + audit), wrapping the policy
// in the failsafe read-only fallback. It returns the Submitter, the failsafe control
// (so the portal can trip/report it) and the audit logger.
//
// override swaps the policy engine: nil selects the default pure-Go RBAC; an adapter
// module passes its own [mantlekeep.PolicyEvaluator] (e.g. OPA) so the heavy engine wires
// in WITHOUT the core importing it. dyn are dynamic action authorizers (the product
// registry) folded into the default RBAC.
func BuildDoor(ctx context.Context, ids mantlekeep.IdentityResolver, override mantlekeep.PolicyEvaluator, dyn ...policy.ActionAuthorizer) (mantlekeep.Submitter, *policy.Failsafe, mantlekeep.AuditLogger) {
	// Default engine is the LIVE (hot-reloadable) RBAC — the resolved cascade sits
	// behind an atomic pointer a watcher can swap with no restart. An adapter override
	// (e.g. OPA) replaces it wholesale; OPA hot-reloads via its own Bundle API.
	var base mantlekeep.PolicyEvaluator = liveRBAC(ctx, dyn...)
	// Eagerly load + validate the merged policy (baseline ∪ platform ∪ products) at BOOT — including
	// the platform SEAL — so a misconfigured policy (e.g. a product doc granting a sealed platform
	// action) FAILS FAST here, not on the first governed request. Idempotent; the RBAC engine reuses
	// the same cache. The lazy readers stay lazy for tests that set the source in TestMain.
	policy.EnsureLoaded()
	if override != nil {
		base = override
		fmt.Println("policy: injected by adapter module (core links no policy engine but RBAC)")
	}
	fs := policy.NewFailsafe(base)
	// Durable, continuous audit chain: a bbolt file in the data dir (NOT wiped on
	// boot). Reopening continues the hash-chain from the last record, so the evidence
	// trail survives restarts — the whole point of "every decision hash-chained".
	aud, err := audit.Open(dataPath("audit.db"))
	must(err)
	return sdk.New(ids, fs, aud), fs, aud
}

// DefaultPolicy returns the core's default pure-Go policy engine (RBAC over the resolved,
// cascaded config). Adapter modules use it to cross-check an alternative engine for drift —
// the two implementations of the one policy MUST agree.
// KnownActions is every action the default policy grants to any role.
//
// Exposed so a parity test in another module can DERIVE what to compare rather than
// hand-listing it: a fixed list only checks the actions somebody remembered to add, and a new
// action can then differ between this policy and authz.rego while the test still passes.
func KnownActions() []string {
	return policy.KnownActions()
}

func DefaultPolicy() mantlekeep.PolicyEvaluator { return configuredRBAC() }

// configuredRBAC builds the default policy engine with its LAYERED runtime config —
// the V3 cascade: MantleKeep default → platform (host) → product → team, most-specific
// wins, with sealed keys the lower layers may only tighten. Pure-Go, near-zero-dep.
//
// Layer order (least specific first) is what policy.Resolve applies:
//   - DefaultLayer()                MantleKeep built-in baseline (unsealed anchor; carries no gates)
//   - MANTLEKEEP_PLATFORM_CONFIG        host/platform layer — its sealed keys are the FLOOR
//   - MANTLEKEEP_TEAM_CONFIG            department/team layer — most specific, but cannot
//     loosen a sealed floor
//   - dyn (product registry)        the product layer, as the action-role fallback
func configuredRBAC(dyn ...policy.ActionAuthorizer) *policy.RBAC {
	base := currentLayers(false)
	resolved := policy.Resolve(base...)
	var fb policy.ActionAuthorizer
	if len(dyn) > 0 && dyn[0] != nil {
		resolved.WithFallback(dyn[0]) // the product registry — product layer of the cascade
		fb = dyn[0]
	}
	// Optional per-scope tier (MANTLEKEEP_SCOPE_CONFIG); no-op when unset — default unchanged.
	eng := attachScopes(policy.NewRBAC().WithResolved(resolved), base, fb, false)
	// Product policy is DATA, read by the generic engine: role grants from the shared grants
	// document and the IT-owned attribute floor (grants/floors.json, MANTLEKEEP_POLICY_FLOORS override).
	// The core imports NO product — nothing to wire here; the engine applies the floor itself.
	return eng
}

// liveRBAC is the HOT-RELOADABLE build of the default engine. It seeds a LiveResolver
// with the boot-time cascade, then starts a Watcher that re-reads the SAME env-configured
// layers and atomically swaps a freshly-resolved cascade on any real change — so an
// action→role binding, an action seal, or a corrected rule goes live with NO restart. The
// sealed floor is re-enforced on every reload because each swap re-runs policy.Resolve.
//
// dyn is the product registry (the product layer), re-attached as the fallback to every
// rebuilt snapshot so runtime-added products keep their RunAs binding across reloads.
func liveRBAC(ctx context.Context, dyn ...policy.ActionAuthorizer) *policy.RBAC {
	var fallback policy.ActionAuthorizer
	if len(dyn) > 0 {
		fallback = dyn[0]
	}

	layers := currentLayers(true) // verbose: emit the per-layer boot log once
	initial := policy.Resolve(layers...)
	if fallback != nil {
		initial.WithFallback(fallback)
	}
	live := policy.NewLiveResolver(initial)

	// The Source re-reads the env-configured files quietly on every poll; a Store-backed
	// Source (bbolt/Postgres) drops in here unchanged for the HA story.
	src := policy.SourceFunc(func(context.Context) ([]policy.Layer, error) {
		return currentLayers(false), nil
	})
	w := policy.NewWatcher(src, live, fallback, layers).
		OnReload(func(revision string, _ *policy.Resolved) {
			// A governance change is itself a governed, evidenced event. We log the
			// revision (content hash) so a later decision is explainable after the fact;
			// the audit hash-chain record is wired where the AuditLogger is available.
			fmt.Printf("policy: hot-reloaded cascade — no restart (revision %s)\n", short(revision))
		})

	// Only spin the poll goroutine when a source can actually change (config files set,
	// or an explicit interval). The bare demo binary with no config starts no watcher —
	// identical to before this change — yet is still built on the live pointer.
	if os.Getenv("MANTLEKEEP_PLATFORM_CONFIG") != "" || os.Getenv("MANTLEKEEP_TEAM_CONFIG") != "" || os.Getenv("MANTLEKEEP_POLICY_RELOAD") != "" {
		iv := reloadInterval()
		w.Start(ctx, iv)
		fmt.Printf("policy: dynamic hot-reload watcher started (poll every %s)\n", iv)
	}
	// Optional per-scope tier (MANTLEKEEP_SCOPE_CONFIG). Known scopes resolve their own tier
	// (static base snapshot); empty/unknown scopes fall through to the live hot-reload path,
	// so this never disturbs base hot-reload. No-op when unset — the default path is unchanged.
	eng := attachScopes(policy.NewRBAC().WithLive(live), layers, fallback, true)
	// Product policy is DATA (role grants + the IT-owned attribute floor), read by the generic
	// engine — the core imports no product, so nothing is wired here. The floor doc has an env
	// override (MANTLEKEEP_POLICY_FLOORS); a DB-backed source drops in behind grants.LoadFloors().
	return eng
}

// currentLayers reads the env-configured cascade (least specific first). It is called
// both at boot (verbose=true, logs each layer once) and on every watcher poll
// (verbose=false, silent) — the single source of truth for "what are the layers now".
func currentLayers(verbose bool) []policy.Layer {
	layers := []policy.Layer{policy.DefaultLayer()}

	// Platform layer FIRST (so its seals bind the team layer that follows), then team.
	if l, ok := loadLayer("MANTLEKEEP_PLATFORM_CONFIG", "platform", verbose); ok {
		layers = append(layers, l)
	}
	if l, ok := loadLayer("MANTLEKEEP_TEAM_CONFIG", "team", verbose); ok {
		layers = append(layers, l)
	}
	return layers
}

// reloadInterval is the watcher poll period from MANTLEKEEP_POLICY_RELOAD (seconds),
// defaulting to 2s. Poll is the honest floor: a change is live in <= this interval.
func reloadInterval() time.Duration {
	if s := os.Getenv("MANTLEKEEP_POLICY_RELOAD"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 2 * time.Second
}

// short trims a content hash to a readable revision prefix for logs.
func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// must aborts on a fatal assembly error — used for unrecoverable boot failures
// (e.g. the audit store cannot open). Serve itself returns errors; this is only for
// the constructors called during wiring.
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
