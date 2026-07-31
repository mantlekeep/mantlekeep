// Package app is MantleKeep packaged as an SDK library — the "starter". A consumer
// imports it, registers the plugins they want, and calls one method:
//
//	a, _ := app.New(ctx,
//	    app.WithPolicy(myPolicyPlugin),      // swap the policy engine
//	    app.WithStore("postgres", pgPlugin), // register a backend plugin
//	)
//	token, err := a.Submit(ctx, intent)      // the one door — you CALL this
//
// The distinction the API encodes:
//   - the SDK (this App) is what you IMPORT and CALL;
//   - plugins are what you REGISTER — the framework calls them through interfaces
//     (inversion of control), never you.
//
// Zero-config runs on sensible embedded defaults (mock identity, a pure-Go generic policy engine
// behind failsafe — NO OPA in the core, bbolt audit, an in-memory store). Each WithX swaps or adds
// a plugin without touching the core (inversion of control).
package app

import (
	"context"
	"os"
	"path/filepath"

	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/internal/audit"
	"mantlekeep.dev/control/internal/identity"
	"mantlekeep.dev/control/internal/policy"
	"mantlekeep.dev/control/internal/provider"
	"mantlekeep.dev/control/internal/sdk"
	"mantlekeep.dev/control/internal/store"
)

// App is the assembled control plane — the SDK surface. Submit is the one door.
type App struct {
	identity mantlekeep.IdentityResolver
	policy   mantlekeep.PolicyEvaluator
	audit    mantlekeep.AuditLogger
	stores   *provider.Registry[mantlekeep.Store]
	door     mantlekeep.Submitter
}

// Option registers or swaps a plugin. Applied by New.
type Option func(*App)

// WithIdentity swaps the identity resolver plugin (mock → AD/LDAP).
func WithIdentity(r mantlekeep.IdentityResolver) Option { return func(a *App) { a.identity = r } }

// WithPolicy swaps the policy-engine plugin (OPA → Cedar/Casbin).
func WithPolicy(p mantlekeep.PolicyEvaluator) Option { return func(a *App) { a.policy = p } }

// WithAudit swaps the audit-logger plugin (bbolt → ClickHouse/S3 WORM).
func WithAudit(l mantlekeep.AuditLogger) Option { return func(a *App) { a.audit = l } }

// WithStore registers a named store plugin; several may coexist and be routed by
// config (see internal/provider Bindings).
func WithStore(name string, s mantlekeep.Store) Option {
	return func(a *App) { a.stores.Register(name, s) }
}

// New assembles the door from the registered plugins, filling any not supplied
// with embedded defaults. Returns an error only if a default fails to build.
func New(ctx context.Context, opts ...Option) (*App, error) {
	a := &App{stores: provider.New[mantlekeep.Store]("store")}
	a.stores.Register("mem", store.NewMem()) // always-available default backend

	for _, o := range opts {
		o(a)
	}

	if a.identity == nil {
		a.identity = identity.NewMock()
	}
	if a.policy == nil {
		// Pure-Go default policy (no OPA in the core), failsafe-wrapped. OPA is an
		// opt-in plugin via WithPolicy(opa.New(...)).
		a.policy = policy.NewFailsafe(policy.NewRBAC())
	}
	if a.audit == nil {
		p := filepath.Join(os.TempDir(), "mantlekeep-sdk-audit.db")
		_ = os.Remove(p)
		aud, err := audit.Open(p)
		if err != nil {
			return nil, err
		}
		a.audit = aud
	}
	a.door = sdk.New(a.identity, a.policy, a.audit)
	return a, nil
}

// Submit is the one door — humans and AI both call this. It is the entire public
// surface a product needs.
func (a *App) Submit(ctx context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	return a.door.Submit(ctx, intent)
}

// Store returns a registered store plugin by name (e.g. from a config binding).
func (a *App) Store(name string) (mantlekeep.Store, error) { return a.stores.Get(name) }

// Stores exposes the store registry for config-driven routing.
func (a *App) Stores() *provider.Registry[mantlekeep.Store] { return a.stores }

// Audit exposes the audit-logger plugin (e.g. to verify the hash chain).
func (a *App) Audit() mantlekeep.AuditLogger { return a.audit }
