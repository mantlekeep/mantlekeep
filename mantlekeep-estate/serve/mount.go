package serve

import (
	"net/http"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// Mounting is what this package hands a deployment that mounts its own endpoints.
//
// The door and the resolver come WITH the mux because this package built both. Handing over only
// the mux forces a deployment to build a second door client and a second caller resolver: two
// identities in the audit for one binary, and two answers to "who is calling". Both silent.
type Mounting struct {
	// Mux is the engine's own mux with the engine's routes ALREADY registered, so a duplicate
	// pattern panics at boot rather than shadowing a route.
	Mux *http.ServeMux
	// Door is the same door client this package submits through.
	Door mantlekeep.Submitter
	// Callers is the same resolver this package answers "who is calling" with.
	Callers api.CallerResolver
}

// routeRegistrar is the engine's own HTTP surface, narrowed to the one method [mount] calls. An
// interface so a test can observe the ORDER of registration without standing up a manager.
type routeRegistrar interface {
	Routes(mux *http.ServeMux)
}

// mountParts is what [Run] has already built that a deployment's endpoints must share rather
// than rebuild. Named fields for the same reason as [managerParts].
type mountParts struct {
	door    mantlekeep.Submitter
	callers api.CallerResolver
}

// mount registers the engine's routes and then the deployment's own, on the one mux.
//
// Extracted from [Run] so it can be TESTED: the order is the guarantee. The engine registers
// FIRST, so a deployment cannot quietly replace one of its routes — ServeMux panics on a
// duplicate pattern, and that panic happens at boot, where an operator sees it, rather than as a
// governed endpoint that silently stopped being the governed one.
func mount(mux *http.ServeMux, engine routeRegistrar, built mountParts, routes func(Mounting)) {
	engine.Routes(mux)
	if routes == nil {
		return
	}
	routes(Mounting{Mux: mux, Door: built.door, Callers: built.callers})
}
