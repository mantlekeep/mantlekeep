package doorclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorserver"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/doorclient"
)

// The client and the door must agree on the wire, and only running both proves it.
//
// # Why this test exists
//
// They did not agree. The door has always sent {"outcome":…,"expiresAt":…,"reasons":[…]} — its own
// package comment says so — while this client read "decision", "expires" and a string "reason".
// The door ALLOWED a change and issued a token, and the client threw the answer away because it
// could not find a field named "decision", reporting "HTTP 200 with no decision" as though the
// door were broken.
//
// Every test on both sides passed throughout. Each module tested its own half against its own
// idea of the contract, and a shared wrong idea tests green forever. Nothing ran the pair.
//
// So this test uses the REAL doorserver, not a stub of it. A stub would have been written from the
// same wrong assumption and would still be passing.
func TestAnAllowSurvivesTheRealDoor(t *testing.T) {
	server, client := realDoorAndClient(t)
	defer server.Close()

	token, err := client.Submit(context.Background(), mantlekeep.Intent{
		ID: "RT-1", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "the client and the door agree on the wire"},
	})
	if err != nil {
		t.Fatalf("a super-admin job.run must be allowed through the real door: %v", err)
	}
	if token.Value == "" {
		t.Fatal("an allow must carry an execution token — an empty one lets an adapter act with nothing behind it")
	}
	if token.IntentID == "" {
		t.Fatal("the token must carry the id the CHAIN recorded, or a caller cites a record that does not exist")
	}
}

// A refusal survives as a REFUSAL, carrying the door's own outcome.
//
// The failure this guards is subtle and worse than a crash: a mis-read refusal becomes "the door
// is broken" rather than "you were denied", so an operator goes looking for an outage instead of
// reading the policy.
func TestARefusalSurvivesTheRealDoorAsARefusal(t *testing.T) {
	server, client := realDoorAndClient(t)
	defer server.Close()

	_, err := client.Submit(context.Background(), mantlekeep.Intent{
		ID: "RT-2", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "nobody"},
		Spec:    mantlekeep.IntentSpec{Goal: "a caller holding no role"},
	})
	if err == nil {
		t.Fatal("a caller with no role must be refused")
	}
	if _, carried := mantlekeep.DecisionFrom(err); !carried {
		t.Fatalf("the refusal must carry the door's decision, not read as a broken door: %v", err)
	}
}

// realDoorAndClient wires the actual doorserver to the actual doorclient.
func realDoorAndClient(t *testing.T) (*httptest.Server, *doorclient.Client) {
	t.Helper()

	engine, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("assembling the door: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := engine.Audit.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	door, err := doorserver.New(doorserver.Options{
		Door: engine,

		// The client authenticates as a SERVICE and names the person in a header. Without an
		// Authenticator the door cannot read its Authorization credential at all — it refuses
		// Authorization as an identity on purpose, because that header carries a secret and its
		// value would become the caller id on the chain.
		//
		// This is the same wiring a real deployment needs, which is the point: a test that
		// skipped it would not be testing the path anything actually uses.
		Authenticator:     serviceCredential("mantlekeep-estate"),
		TrustedUserHeader: "X-Caller",
		// The client sends the subject as X-On-Behalf-Of, and names itself in Authorization.
		DelegatedSubjectHeader: "X-On-Behalf-Of",
		Delegators:             []string{"mantlekeep-estate"},
	})
	if err != nil {
		t.Fatalf("building the door server: %v", err)
	}

	server := httptest.NewServer(door.Handler())
	return server, doorclient.New(server.URL, "mantlekeep-estate")
}

// serviceCredential is the smallest authenticator that verifies rather than assumes.
//
// It accepts one bearer value and reports the service it belongs to. A real deployment verifies a
// signed token instead; the door does not care which, because it only ever asked one question —
// which principal is this — and both answer it.
type serviceCredential string

func (s serviceCredential) Authenticate(request *http.Request) (string, error) {
	presented := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	if presented != string(s) {
		// Never quote what was presented: it is a credential, and an error string reaches logs.
		return "", errors.New("the presented credential does not verify")
	}
	return string(s), nil
}
