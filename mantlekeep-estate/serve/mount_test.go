package serve

import (
	"context"
	"net/http"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// recordingEngine stands in for the engine's routes and remembers WHEN it registered, so a test
// can tell "after" from "instead of".
type recordingEngine struct {
	registeredOn *http.ServeMux
}

func (e *recordingEngine) Routes(mux *http.ServeMux) { e.registeredOn = mux }

// identityDoor is a door with an identity, so a test can ask "is this THE door" rather than "is
// this a door". A zero-size value would compare equal to every other one and prove nothing.
type identityDoor struct{ name string }

func (*identityDoor) Submit(context.Context, mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	return mantlekeep.ExecutionToken{Value: "test", IntentID: "INT-1"}, nil
}

// THE test for this option: a deployment's endpoints are mounted AFTER the engine's, on the same
// mux, with the same door and the same caller resolver.
//
// Each half guards a quiet failure. Before the engine, and a deployment can shadow a governed
// route. A different mux, and the endpoints answer nothing. A different door or resolver, and one
// binary has two identities in the audit and two answers to "who is calling".
func TestADeploymentsRoutesMountAfterTheEnginesWithTheSameParts(t *testing.T) {
	mux := http.NewServeMux()
	engine := &recordingEngine{}
	door := &identityDoor{name: "the engine's door"}
	callers := &stubResolver{}

	var handed *Mounting
	mount(mux, engine, mountParts{door: door, callers: callers}, func(mounting Mounting) {
		if engine.registeredOn == nil {
			t.Error("the deployment's routes were mounted BEFORE the engine's — a deployment " +
				"could then shadow a governed route")
		}
		handed = &mounting
	})

	if handed == nil {
		t.Fatal("Options.Routes is set and was never called")
	}
	if handed.Mux != mux || engine.registeredOn != mux {
		t.Error("the deployment was handed a different mux from the one the engine serves on — " +
			"its endpoints would answer nothing")
	}
	if handed.Door != door {
		t.Errorf("the deployment was handed door %v, want the engine's own %v", handed.Door, door)
	}
	if handed.Callers != callers {
		t.Errorf("the deployment was handed resolver %v, want the engine's own %v",
			handed.Callers, callers)
	}
}

// A deployment that registers a pattern the engine already serves must fail at BOOT, loudly. The
// alternative is a governed endpoint that silently stopped being the governed one.
//
// The real engine's routes, not a stand-in: what is being proven is that no deployment can
// replace one of THOSE.
func TestARouteThatDuplicatesTheEnginesPanicsAtRegistration(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a deployment registered POST /api/estate/{team} over the engine's own " +
				"and nothing objected")
		}
	}()

	mount(http.NewServeMux(), api.New(nil, nil, &stubResolver{}), mountParts{},
		func(mounting Mounting) {
			mounting.Mux.HandleFunc("POST /api/estate/{team}", func(http.ResponseWriter, *http.Request) {})
		})
}

// Nil is what every deployment had before this option existed: the engine's routes, and nothing
// else.
func TestNilRoutesMountsOnlyTheEngine(t *testing.T) {
	mux := http.NewServeMux()
	engine := &recordingEngine{}

	mount(mux, engine, mountParts{}, nil)

	if engine.registeredOn != mux {
		t.Fatal("with no deployment routes, the engine's own were not registered")
	}
}
