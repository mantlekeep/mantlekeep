package doorserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
)

// A service account frequently acts FOR a person: the service authenticates, the person
// is who the action belongs to. Both must reach the chain — the person as the subject,
// the service as `via` — and only a permitted delegator may make that claim.

func newTestServer(t *testing.T, delegators ...string) http.Handler {
	t.Helper()
	door, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(t.TempDir()) })

	server, err := New(Options{
		Door:                   door,
		TrustedUserHeader:      "X-Caller",
		DelegatedSubjectHeader: "X-On-Behalf-Of",
		Delegators:             delegators,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

func govern(t *testing.T, handler http.Handler, callerID, onBehalfOf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/govern",
		strings.NewReader(`{"action":"job.run","resource":"r","goal":"do the thing"}`))
	request.Header.Set("X-Caller", callerID)
	if onBehalfOf != "" {
		request.Header.Set("X-On-Behalf-Of", onBehalfOf)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestPermittedDelegatorActsForAPerson(t *testing.T) {
	handler := newTestServer(t, "cp-service")

	// root is the engine-baked superadmin, so the decision itself is an allow and the
	// test is about WHOSE action it was recorded as.
	if got := govern(t, handler, "cp-service", "root").Code; got != http.StatusOK {
		t.Fatalf("a permitted delegator should be allowed to act for a person, got %d", got)
	}

	// The chain must name the person as subject and the service as via.
	// Read the chain as a person in the directory. A service account is authenticated by
	// the gateway but need not exist as a human identity, so it cannot read the audit
	// itself — least privilege, and worth knowing before wiring an ops dashboard.
	auditRequest := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	auditRequest.Header.Set("X-Caller", "root")
	auditRecorder := httptest.NewRecorder()
	handler.ServeHTTP(auditRecorder, auditRequest)

	var view struct {
		Records []struct {
			Subject string `json:"subject"`
			Via     string `json:"via"`
		} `json:"records"`
	}
	if err := json.Unmarshal(auditRecorder.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Records) == 0 {
		t.Fatal("the delegated action was not recorded at all")
	}
	record := view.Records[0]
	if record.Subject != "root" {
		t.Errorf("subject should be the person acted for, got %q", record.Subject)
	}
	if record.Via != "cp-service" {
		t.Errorf("via should name the service that carried the claim, got %q", record.Via)
	}
}

func TestUnpermittedCallerCannotActForSomeoneElse(t *testing.T) {
	// No delegators configured: nobody may speak for anyone else.
	handler := newTestServer(t)

	recorder := govern(t, handler, "dev-alice", "root")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("impersonation must be REFUSED, got %d — a caller able to name any "+
			"subject makes the audit trail a lie", recorder.Code)
	}
	// It must not silently downgrade to acting as itself: that would turn a privilege
	// violation into a successful request.
	if strings.Contains(recorder.Body.String(), `"allow"`) {
		t.Error("an overreaching delegation was allowed through as the caller itself")
	}
}

func TestActingAsYourselfNeedsNoDelegation(t *testing.T) {
	handler := newTestServer(t) // no delegators at all

	if got := govern(t, handler, "root", "").Code; got != http.StatusOK {
		t.Fatalf("a caller acting as itself should be unaffected by delegation, got %d", got)
	}
}

func TestDelegatorNamingAnUnknownPersonIsRefused(t *testing.T) {
	handler := newTestServer(t, "cp-service")

	recorder := govern(t, handler, "cp-service", "nobody-in-the-directory")

	if recorder.Code == http.StatusOK {
		t.Fatal("a trusted delegator must not be able to record an action against an " +
			"identity the directory cannot confirm")
	}
}
