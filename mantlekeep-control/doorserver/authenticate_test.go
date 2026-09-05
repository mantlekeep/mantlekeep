package doorserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// verifierOf accepts exactly one credential, standing in for a real token check.
type verifierOf struct{ token, id string }

func (v verifierOf) Authenticate(request *http.Request) (string, error) {
	if request.Header.Get("Authorization") != "Bearer "+v.token {
		return "", fmt.Errorf("credential does not verify")
	}
	return v.id, nil
}

func requestWith(headers map[string]string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/govern", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request
}

// THE attack the door's delegation model exposes. Trusting a header for identity lets a
// caller be one person; trusting it for DELEGATOR status lets them be the service that speaks
// for everyone — requester and approver at once, recorded as fact.
func TestAVerifiedDoorRefusesAHeaderClaimingToBeADelegator(t *testing.T) {
	server := &Server{options: Options{
		Authenticator:          verifierOf{token: "real-service-token", id: "mantlekeep-estate"},
		TrustedUserHeader:      "X-Caller",
		DelegatedSubjectHeader: "X-On-Behalf-Of",
		Delegators:             []string{"mantlekeep-estate"},
	}}

	// No credential, but every header an attacker would set.
	id, ok := server.authenticatedCaller(requestWith(map[string]string{
		"X-Caller":       "mantlekeep-estate",
		"X-On-Behalf-Of": "arch-carol",
	}))
	if ok {
		t.Fatalf("an unverified caller authenticated as %q — it could now delegate as anyone", id)
	}
}

// A configured Authenticator must be the ONLY path. Falling back to the header on a
// verification failure makes the weaker tier the one an attacker chooses.
func TestAVerifiedDoorDoesNotFallBackToTheHeader(t *testing.T) {
	server := &Server{options: Options{
		Authenticator:     verifierOf{token: "real", id: "dev-alice"},
		TrustedUserHeader: "X-Caller",
	}}
	if _, ok := server.authenticatedCaller(requestWith(map[string]string{
		"Authorization": "Bearer forged",
		"X-Caller":      "dev-alice",
	})); ok {
		t.Fatal("a failed verification fell back to trusting the header")
	}
}

func TestAVerifiedCredentialAuthenticates(t *testing.T) {
	server := &Server{options: Options{
		Authenticator: verifierOf{token: "real", id: "dev-alice"},
	}}
	id, ok := server.authenticatedCaller(requestWith(map[string]string{
		"Authorization": "Bearer real",
	}))
	if !ok || id != "dev-alice" {
		t.Fatalf("a valid credential gave (%q, %v)", id, ok)
	}
}

// A framework that cannot be started in five minutes does not get tried. Loopback keeps
// working with no identity provider at all.
func TestLoopbackKeepsTheUnverifiedTiers(t *testing.T) {
	for _, addr := range []string{"localhost:8080", "127.0.0.1:8080", "[::1]:8080"} {
		if err := checkIdentityTier(Options{TrustedUserHeader: "X-Caller", DevLogin: true},
			addr, false); err != nil {
			t.Errorf("%s was refused with no IdP: %v", addr, err)
		}
	}
}

// THE fence. Off loopback, the delegation list checks a name the header does not prove.
func TestOffLoopbackRefusesUnverifiedIdentity(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "10.1.2.3:8080"} {
		err := checkIdentityTier(Options{TrustedUserHeader: "X-Caller"}, addr, false)
		if err == nil {
			t.Errorf("%s accepted an unverified identity tier", addr)
		}
	}
}

// Dev-login mints a session for any named user with no credential check at all. Off loopback
// it is the same hole with a friendlier interface.
func TestOffLoopbackRefusesDevLoginToo(t *testing.T) {
	if err := checkIdentityTier(Options{DevLogin: true}, ":8080", false); err == nil {
		t.Fatal("dev-login was accepted on a non-loopback address")
	}
}

// The escape hatch exists — a gateway genuinely being the only path is a real deployment —
// but it must be chosen by name.
func TestTheUnverifiedTierCanBeChosenOffLoopback(t *testing.T) {
	if err := checkIdentityTier(Options{TrustedUserHeader: "X-Caller"}, ":8080", true); err != nil {
		t.Fatalf("the named escape hatch was refused: %v", err)
	}
}

// A verifying door needs no fence: it does not depend on the network path being what it was
// designed to be.
func TestAVerifyingDoorIsAllowedAnywhere(t *testing.T) {
	if err := checkIdentityTier(Options{Authenticator: verifierOf{}}, ":8080", false); err != nil {
		t.Fatalf("a verifying door was refused off loopback: %v", err)
	}
}

// The refusal must say what to do, or the next thing an operator tries is the setting that
// turns the check off.
func TestTheRefusalNamesTheWayForward(t *testing.T) {
	err := checkIdentityTier(Options{TrustedUserHeader: "X-Caller"}, ":8080", false)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"Authenticator", TrustHeaderVar} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}
