package policy

import "sync"

// ScopeResolver selects the effective policy cascade PER SCOPE at request time.
//
// A "scope" is a GENERIC tenancy key on the intent — the SDLC product maps its *project*
// onto it, but another product may use a tenant, workspace, app, or namespace, and a
// product with no tenancy leaves it empty. The core knows nothing about "projects"; it only
// resolves the scope's cascade tier. That keeps this generic and reusable, not SDLC-specific.
//
// The base layers (MantleKeep default → product → team) are SHARED across every scope; each
// scope may add its OWN layer on top. Resolve() cascades base + the scope's layer with the
// sealed floor enforced, so a scope can override its free rows but never loosen a sealed key.
// Resolutions are cached per scope; a change invalidates that entry (a hot-reload swaps the
// whole resolver). An unknown or empty scope resolves the base alone — so a scope-less intent
// still gets the shared floor, and a scope that hasn't customised anything behaves like its team.
type ScopeResolver struct {
	mu       sync.RWMutex
	base     []Layer
	scopes   map[string]Layer     // scope key → its override layer
	cache    map[string]*Resolved // scope key (or "") → resolved cascade
	fallback ActionAuthorizer     // consulted after the resolved roles (e.g. the product registry)
	ladder   RoleLadder           // the deployment's role vocabulary, passed to every per-scope Resolve
}

// NewScopeResolver seeds the shared base cascade (default → product → team). ladder is the
// deployment's role vocabulary — every per-scope resolution ranks against the SAME ladder, so a
// renamed-role deployment's per-scope seal checks use the same vocabulary as the base.
func NewScopeResolver(ladder RoleLadder, base ...Layer) *ScopeResolver {
	return &ScopeResolver{base: base, scopes: map[string]Layer{}, cache: map[string]*Resolved{}, ladder: ladder}
}

// WithFallback sets the authorizer every per-scope resolution chains to when a layer does not
// define an action's role — the product registry. Without this, wiring per-scope would drop
// runtime-added products' RunAs bindings. Returns the receiver. Clears the cache so the
// fallback is applied to future resolutions.
func (p *ScopeResolver) WithFallback(a ActionAuthorizer) *ScopeResolver {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fallback = a
	p.cache = map[string]*Resolved{}
	return p
}

// Has reports whether a scope has its own override layer. The engine uses this to send only
// KNOWN scopes through per-scope resolution and let everything else fall through to the live
// (hot-reloadable) base — so wiring scopes never disturbs the base hot-reload path.
func (p *ScopeResolver) Has(key string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.scopes[key]
	return ok
}

// SetScope installs (or replaces) a scope's override layer and invalidates its cache.
func (p *ScopeResolver) SetScope(key string, l Layer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scopes[key] = l
	delete(p.cache, key)
}

// For returns the effective cascade for a scope — base (+ the scope's layer if any), cached.
// Empty/unknown scope → the base alone. Never nil.
func (p *ScopeResolver) For(scope string) *Resolved {
	p.mu.RLock()
	if r, ok := p.cache[scope]; ok {
		p.mu.RUnlock()
		return r
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.cache[scope]; ok {
		return r
	}
	layers := append([]Layer{}, p.base...)
	if l, ok := p.scopes[scope]; ok {
		layers = append(layers, l)
	}
	res := Resolve(p.ladder, layers...)
	if p.fallback != nil {
		res.WithFallback(p.fallback) // keep the product-registry binding per scope
	}
	p.cache[scope] = res
	return res
}

// ensure Resolved satisfies ActionAuthorizer (used by the engine per request).
var _ ActionAuthorizer = (*Resolved)(nil)
