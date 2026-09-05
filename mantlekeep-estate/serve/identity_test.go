package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// A framework that cannot be started in five minutes does not get tried. Loopback with no
// identity provider must keep working — that is the laptop, the demo, and an air-gapped
// first day.
func TestLoopbackRunsWithNoIdentityProvider(t *testing.T) {
	for _, addr := range []string{"localhost:8092", "127.0.0.1:8092", "[::1]:8092"} {
		callers, err := resolveCallers(addr)
		if err != nil {
			t.Errorf("%s was refused with no IdP configured: %v", addr, err)
		}
		if _, ok := callers.(api.HeaderCallers); !ok {
			t.Errorf("%s did not get the header resolver, got %T", addr, callers)
		}
	}
}

// THE fence. Off loopback, an unverified header is an open door to anything that can route
// to the port — and the failure is silent, because a forged identity produces a perfectly
// ordinary-looking record.
func TestOffLoopbackRefusesTheTrustedHeader(t *testing.T) {
	for _, addr := range []string{":8092", "0.0.0.0:8092", "10.1.2.3:8092"} {
		if _, err := resolveCallers(addr); err == nil {
			t.Errorf("%s accepted the trusted-header resolver with no verification", addr)
		}
	}
}

// The escape hatch exists, because a gateway genuinely being the only path is a real
// deployment. It must be chosen by name, not inherited.
func TestTheHeaderCanBeTrustedOffLoopbackWhenChosen(t *testing.T) {
	t.Setenv(TrustHeaderVar, "true")
	callers, err := resolveCallers(":8092")
	if err != nil {
		t.Fatalf("the named escape hatch was refused: %v", err)
	}
	if _, ok := callers.(api.HeaderCallers); !ok {
		t.Errorf("got %T, want the header resolver", callers)
	}
}

// A partly-configured verifier is worse than none: it looks like verification while
// accepting tokens it should refuse, and nobody reviews a deployment that appears to verify.
func TestAPartlyConfiguredVerifierIsRefused(t *testing.T) {
	t.Setenv(IssuerVar, "https://idp.example.com")
	// audience and JWKS deliberately unset
	if _, err := resolveCallers("localhost:8092"); err == nil {
		t.Fatal("a verifier with only an issuer was accepted")
	}
}

// Fully configured, the estate verifies for itself and no longer depends on the network path
// being what it was designed to be.
func TestAFullyConfiguredVerifierIsUsed(t *testing.T) {
	jwks := filepath.Join(t.TempDir(), "jwks.json")
	// A real RSA JWKS, minimal but well-formed.
	document := `{"keys":[{"kid":"k1","kty":"RSA","n":"` +
		"0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw" + `","e":"AQAB"}]}`
	if err := os.WriteFile(jwks, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(IssuerVar, "https://idp.example.com")
	t.Setenv(AudienceVar, "mantlekeep-estate")
	t.Setenv(JWKSVar, jwks)

	callers, err := resolveCallers(":8092") // off loopback — verification makes that fine
	if err != nil {
		t.Fatalf("a configured verifier was refused: %v", err)
	}
	verifier, ok := callers.(*api.VerifiedCallers)
	if !ok {
		t.Fatalf("got %T, want the verifying resolver", callers)
	}
	if verifier.Audience != "mantlekeep-estate" {
		t.Errorf("audience = %q", verifier.Audience)
	}
}

// The refusal must say what to do, or the next thing an operator tries is the setting that
// turns the check off.
func TestTheRefusalNamesTheWayForward(t *testing.T) {
	_, err := resolveCallers(":8092")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{IssuerVar, AudienceVar, JWKSVar, TrustHeaderVar} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %s: %v", want, err)
		}
	}
}
