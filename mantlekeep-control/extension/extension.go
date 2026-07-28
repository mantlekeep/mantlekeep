// Package extension is the portal's route-mount SEAM. A product — or an adapter
// module the host builds in its OWN binary — supplies its own HTTP endpoints as a
// RouteProvider, and the portal mounts them at startup. So the generic core never
// grows host-specific handlers: the host adds endpoints by injecting a provider via
// app.Options.Extensions (the same pattern mantlekeep-opa uses to inject a policy engine),
// not by editing core. This is the API analog of the runtime web overlay — extend the
// surface without touching the core binary.
//
// This package is PUBLIC (not internal) precisely so a separate module can implement
// RouteProvider; it depends only on net/http.
package extension

import "net/http"

// RouteRegistrar is the minimal mux the portal hands to a provider. method is an HTTP
// method ("GET"/"POST"/…); pattern is a Go 1.22 ServeMux pattern (may carry {vars}).
type RouteRegistrar interface {
	Handle(method, pattern string, h http.HandlerFunc)
}

// RouteProvider mounts a set of routes. Implement it in your product or module and pass
// the value via app.Options.Extensions; the portal calls MountRoutes once at startup.
// A provider should namespace its patterns (e.g. /api/<yours>/…) to avoid colliding
// with core routes.
type RouteProvider interface {
	MountRoutes(r RouteRegistrar)
}

// Routes is a convenience RouteProvider: a plain list of routes. Build one with Route
// values instead of writing a type. func handlers keep their own closure state.
type Routes []Route

// Route is one method+pattern+handler triple.
type Route struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
}

// MountRoutes registers every route in the list.
func (rs Routes) MountRoutes(r RouteRegistrar) {
	for _, rt := range rs {
		r.Handle(rt.Method, rt.Pattern, rt.Handler)
	}
}
