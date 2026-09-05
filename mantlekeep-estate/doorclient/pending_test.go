package doorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// The 409 branch downstream was only ever proven against a hand-written fixture, because the
// real door collapsed every refusal into {"decision":"deny"} with 403. This drives the shape the
// door NOW emits and asserts the decision survives the wire — which is what makes a pending
// approval distinguishable from a denial anywhere upstream.
func TestARequireApprovalRefusalKeepsItsDecisionAcrossTheWire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"decision":"require_approval",
		  "reason":"a platform approver must sign off",
		  "requiredApprovers":["L1-Architect"]}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "mantlekeep-estate").Submit(context.Background(), mantlekeep.Intent{
		ID: "ESTATE-payments-orders-1", Action: "estate.apply", Resource: "team/payments",
	})
	if err == nil {
		t.Fatal("a refusal must be an error")
	}
	if !strings.HasPrefix(err.Error(), "require_approval: ") {
		t.Fatalf("the door's own decision must survive; got %q", err)
	}
	if strings.HasPrefix(err.Error(), "deny") {
		t.Fatal("a pending approval reported as a denial — nobody would go looking for an approver")
	}
}

// The recorded id, not the one we sent. A caller that cites the id it hoped for is one door
// change away from naming a record that does not exist.
func TestTheIdReportedIsTheOneTheChainRecorded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"allow","token":"tok-1",
		  "expires":"2030-01-01T00:00:00Z","intentId":"RECORDED-BY-THE-DOOR"}`))
	}))
	defer server.Close()

	token, err := New(server.URL, "mantlekeep-estate").Submit(context.Background(), mantlekeep.Intent{
		ID: "WHAT-WE-ASKED-FOR", Action: "estate.apply", Resource: "team/payments",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if token.IntentID != "RECORDED-BY-THE-DOOR" {
		t.Errorf("IntentID = %q — a caller must report what the chain holds, not what it sent",
			token.IntentID)
	}
}
