package serve

import (
	"net/http"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// stubResolver stands in for whatever a binary injects — a token verifier from
// mantlekeep-oidc, a mesh identity, anything. This module deliberately carries none of them,
// which is why the test supplies one rather than configuring one.
type stubResolver struct{}

func (stubResolver) Caller(*http.Request) (mantlekeep.Subject, error) {
	return mantlekeep.Subject{ID: "verified-person"}, nil
}

// A framework that cannot be started in five minutes does not get tried. Loopback with
// nothing supplied must keep working — that is the laptop, the demo, and an air-gapped first
// day.
func TestLoopbackRunsWithNoResolverSupplied(t *testing.T) {
	for _, addr := range []string{"localhost:8092", "127.0.0.1:8092", "[::1]:8092"} {
		callers, err := resolveCallers(addr, nil)
		if err != nil {
			t.Errorf("%s was refused with nothing supplied: %v", addr, err)
		}
		if _, ok := callers.(api.HeaderCallers); !ok {
			t.Errorf("%s did not get the header resolver, got %T", addr, callers)
		}
	}
}

// THE fence. Off loopback an unverified header is an open door to anything that can route to
// the port, and the failure is silent — a forged identity produces a perfectly ordinary
// record.
func TestOffLoopbackRefusesTheTrustedHeader(t *testing.T) {
	for _, addr := range []string{":8092", "0.0.0.0:8092", "10.1.2.3:8092"} {
		if _, err := resolveCallers(addr, nil); err == nil {
			t.Errorf("%s accepted the trusted-header resolver with nothing verifying", addr)
		}
	}
}

// The escape hatch exists — a gateway genuinely being the only path is a real deployment —
// but it must be chosen by name rather than inherited.
func TestTheHeaderCanBeTrustedOffLoopbackWhenChosen(t *testing.T) {
	t.Setenv(TrustHeaderVar, "true")
	callers, err := resolveCallers(":8092", nil)
	if err != nil {
		t.Fatalf("the named escape hatch was refused: %v", err)
	}
	if _, ok := callers.(api.HeaderCallers); !ok {
		t.Errorf("got %T, want the header resolver", callers)
	}
}

// A supplied resolver needs no fence: a binary that verifies does not depend on the network
// path being what it was designed to be.
func TestASuppliedResolverIsUsedAndNeedsNoFence(t *testing.T) {
	callers, err := resolveCallers(":8092", stubResolver{})
	if err != nil {
		t.Fatalf("a supplied resolver was refused off loopback: %v", err)
	}
	if _, ok := callers.(stubResolver); !ok {
		t.Errorf("got %T, want the supplied resolver", callers)
	}
}

// The refusal must say what to do, or the next thing an operator tries is the setting that
// turns the check off.
func TestTheRefusalNamesTheWayForward(t *testing.T) {
	_, err := resolveCallers(":8092", nil)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"Options.Callers", TrustHeaderVar} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}
