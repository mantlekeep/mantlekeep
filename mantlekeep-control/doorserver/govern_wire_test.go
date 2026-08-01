package doorserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mantlekeep.dev/control/doorkit"
)

// The /api/govern response is the enterprise wire contract: an outcome (allow / deny /
// require_approval), the policy that decided, typed reasons, the approvers a
// require_approval needs, and when an allow expires. A flat {decision, token} cannot carry
// what an auditor asks — who must approve, under which policy, why, valid until when. These
// tests pin the canonical shape.

// governWith drives one request with an explicit body and caller, returning the parsed
// JSON response and the HTTP status.
func governWith(t *testing.T, handler http.Handler, caller, body string) (map[string]any, int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/govern", strings.NewReader(body))
	request.Header.Set("X-Caller", caller)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var parsed map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("response is not JSON (%v): %s", err, recorder.Body.String())
	}
	return parsed, recorder.Code
}

func devLoginServer(t *testing.T) http.Handler {
	t.Helper()
	door, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(t.TempDir()) })
	server, err := New(Options{Door: door, TrustedUserHeader: "X-Caller"})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

func TestAllowCarriesTheEnterpriseFields(t *testing.T) {
	handler := devLoginServer(t)

	// root is the engine-baked superadmin — a clean allow.
	body, status := governWith(t, handler, "root",
		`{"action":"job.run","resource":"project/demo","goal":"ship it"}`)

	if status != http.StatusOK {
		t.Fatalf("allow should be 200, got %d: %v", status, body)
	}
	if body["outcome"] != "allow" {
		t.Errorf("outcome should be \"allow\", got %v", body["outcome"])
	}
	if body["token"] == nil || body["token"] == "" {
		t.Error("an allow must carry a token")
	}
	if body["policyId"] == nil || body["policyId"] == "" {
		t.Errorf("an allow must name the policy that authorised it, got %v", body["policyId"])
	}
	if body["expiresAt"] == nil || body["expiresAt"] == "" {
		t.Error("an allow must say when the authorisation expires")
	}
	// reasons is present and empty on allow (a stable shape a client can always read).
	if _, ok := body["reasons"]; !ok {
		t.Error("reasons must be present (empty) on allow")
	}
}

func TestDenyIsTypedAndForbidden(t *testing.T) {
	handler := devLoginServer(t)

	// dev-alice holds only L3-Consumer; the core ships no grant for job.run, so the
	// policy denies with "no role permits".
	body, status := governWith(t, handler, "dev-alice",
		`{"action":"job.run","resource":"project/demo","goal":"try it"}`)

	if status != http.StatusForbidden {
		t.Fatalf("a policy deny should be 403, got %d: %v", status, body)
	}
	if body["outcome"] != "deny" {
		t.Errorf("outcome should be \"deny\", got %v", body["outcome"])
	}
	reasons, ok := body["reasons"].([]any)
	if !ok || len(reasons) == 0 {
		t.Fatalf("a deny must carry at least one typed reason, got %v", body["reasons"])
	}
	first, _ := reasons[0].(map[string]any)
	if first["code"] != "DENY_ACTION_NOT_ALLOWED" {
		t.Errorf("an ungranted action should be DENY_ACTION_NOT_ALLOWED, got %v", first["code"])
	}
	if first["message"] == nil || first["message"] == "" {
		t.Error("a typed reason must still carry a human message")
	}
}

func TestMissingGoalIsAValidationDenyAt400(t *testing.T) {
	handler := devLoginServer(t)

	// No goal — declare-before-execute is a framework floor, refused before policy.
	body, status := governWith(t, handler, "root",
		`{"action":"job.run","resource":"project/demo"}`)

	if status != http.StatusBadRequest {
		t.Fatalf("a validation refusal should be 400, got %d: %v", status, body)
	}
	if body["outcome"] != "deny" {
		t.Errorf("outcome should be \"deny\", got %v", body["outcome"])
	}
	reasons, _ := body["reasons"].([]any)
	if len(reasons) == 0 {
		t.Fatal("a validation deny must carry a typed reason")
	}
	first, _ := reasons[0].(map[string]any)
	if first["code"] != "DENY_VALIDATION" {
		t.Errorf("a malformed intent should be DENY_VALIDATION, got %v", first["code"])
	}
}
