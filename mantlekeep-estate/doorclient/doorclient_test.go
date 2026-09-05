package doorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

func intent() mantlekeep.Intent {
	return mantlekeep.Intent{
		ID: "ESTATE-payments-orders-1", Action: "estate.apply", Resource: "team/payments",
		Subject: mantlekeep.Subject{ID: "dev-alice"},
		Spec:    mantlekeep.IntentSpec{Goal: "provision kafka topic"},
		Params:  map[string]any{"tier": "dev", "gate": "none", "scope": "payments"},
	}
}

// The human is asserted in a HEADER and the application authenticates as itself. An actor in
// the body would be an unauthenticated change wearing a name.
func TestTheActorTravelsAsAHeaderAndTheAppAuthenticatesAsItself(t *testing.T) {
	var gotAuthorization, gotOnBehalfOf string
	var body governRequest
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotOnBehalfOf = r.Header.Get(onBehalfOfHeader)
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(governResponse{Decision: "allow", Token: "tok-1"})
	}))
	defer door.Close()

	if _, err := New(door.URL, "cp-service").Submit(context.Background(), intent()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if gotAuthorization != "Bearer cp-service" {
		t.Errorf("the application must authenticate as itself, got %q", gotAuthorization)
	}
	if gotOnBehalfOf != "dev-alice" {
		t.Errorf("the human must be asserted in %s, got %q", onBehalfOfHeader, gotOnBehalfOf)
	}
	if body.Params["tier"] != "dev" || body.Params["gate"] != "none" {
		t.Errorf("tier and gate must reach the door so policy can rule on consequence: %+v", body.Params)
	}
	if body.Scope != "payments" {
		t.Errorf("scope = %q, want the team — a policy tier is selected by it", body.Scope)
	}
}

// A refusal must arrive in the door's own words and the door's own shape, so one classifier
// serves an in-process door and a remote one alike.
func TestARefusalCarriesTheDoorsOwnWords(t *testing.T) {
	const reason = "estate floor: a platform-gated change needs a platform approver"
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(governResponse{Decision: "deny", Reason: reason})
	}))
	defer door.Close()

	_, err := New(door.URL, "cp-service").Submit(context.Background(), intent())
	if err == nil {
		t.Fatal("a denied intent returned no error")
	}
	if err.Error() != "deny: "+reason {
		t.Errorf("refusal = %q, want the in-process door's exact shape %q", err, "deny: "+reason)
	}
}

func TestARequireApprovalKeepsItsDecisionWord(t *testing.T) {
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(governResponse{
			Decision: "require_approval", Reason: "a platform approver must sign off"})
	}))
	defer door.Close()

	_, err := New(door.URL, "cp-service").Submit(context.Background(), intent())
	if err == nil || !strings.HasPrefix(err.Error(), "require_approval:") {
		t.Fatalf("require_approval must survive as its own decision word, got %v", err)
	}
}

// The one that must never be confused: the door did not refuse, the door did not answer.
func TestAnUnreachableDoorIsNotARefusal(t *testing.T) {
	door := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := door.URL
	door.Close() // nothing is listening now

	_, err := New(address, "cp-service").Submit(context.Background(), intent())
	if err == nil {
		t.Fatal("an unreachable door returned no error")
	}
	message := err.Error()
	if strings.HasPrefix(message, "deny:") || strings.HasPrefix(message, "require_approval:") {
		t.Fatalf("a transport failure was worded as a decision (%q) — a network fault would be "+
			"recorded as a governance outcome", message)
	}
	if !strings.Contains(message, "unreachable") {
		t.Errorf("the failure must say the door was not reached, got %q", message)
	}
}

// An allow with no token is not an allow: the adapter would be handed nothing to act under.
func TestAnAllowWithNoTokenIsRefused(t *testing.T) {
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(governResponse{Decision: "allow"})
	}))
	defer door.Close()

	if _, err := New(door.URL, "cp-service").Submit(context.Background(), intent()); err == nil {
		t.Fatal("an allow carrying no execution token was accepted")
	}
}
