package doorclient_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorserver"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/doorclient"
)

// A door that trusts a header and has NO Authenticator — the shape a door behind an
// authenticating proxy takes — never reads Authorization. Through the real doorserver, a
// client without a caller header is refused as unauthenticated, and the same client WITH one
// is allowed as a delegator acting for the person.
func TestTheCallerHeaderAuthenticatesToAHeaderTrustingDoor(t *testing.T) {
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
		Door:                   engine,
		TrustedUserHeader:      "X-Caller",
		DelegatedSubjectHeader: "X-On-Behalf-Of",
		Delegators:             []string{"platform-estate"},
	})
	if err != nil {
		t.Fatalf("building the door server: %v", err)
	}
	server := httptest.NewServer(door.Handler())
	defer server.Close()

	intent := mantlekeep.Intent{
		ID: "CH-1", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "the door reads who is calling"},
	}

	_, err = doorclient.New(server.URL, "platform-estate").Submit(context.Background(), intent)
	if err == nil {
		t.Fatal("a header-trusting door allowed a client that presented no caller header")
	}
	if !strings.Contains(err.Error(), "no caller identity") {
		t.Fatalf("got %v, want the door's unauthenticated refusal", err)
	}

	client, err := doorclient.NewWithOptions(server.URL, "platform-estate",
		doorclient.WithCallerHeader("X-Caller"))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	intent.ID = "CH-2"
	token, err := client.Submit(context.Background(), intent)
	if err != nil {
		t.Fatalf("the door refused a client presenting its service account in the trusted header: %v", err)
	}
	if token.Value == "" {
		t.Fatal("an allow must include an execution token")
	}
}
