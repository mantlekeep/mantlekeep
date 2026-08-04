package doorserver

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// captureLog redirects the default slog logger to a buffer for the duration of a test and
// returns the buffer. It restores the previous default on cleanup so tests do not bleed into
// one another.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}

// TestDecisionLogAllowIsStructuredAndLeaksNoSecret drives an allow carrying a secret-looking
// param, and proves: the decision is logged with the expected metadata fields, AND the secret
// param value never reaches the log. The security guarantee is that only decision metadata is
// logged — no intent params, no token.
func TestDecisionLogAllowIsStructuredAndLeaksNoSecret(t *testing.T) {
	handler := devLoginServer(t)
	log := captureLog(t)

	// root is the baked superadmin — a clean allow. The param value is a canary: it must
	// NOT appear anywhere in the log line.
	const secret = "super-secret-token-value-9f3a"
	_, status := governWith(t, handler, "root",
		`{"action":"job.run","resource":"project/demo","goal":"ship it","params":{"password":"`+secret+`"}}`)
	if status != http.StatusOK {
		t.Fatalf("expected an allow (200), got %d", status)
	}

	line := log.String()
	if strings.Contains(line, secret) {
		t.Fatalf("SECURITY: a param value leaked into the decision log:\n%s", line)
	}
	if strings.Contains(line, "password") {
		t.Fatalf("SECURITY: a param key leaked into the decision log:\n%s", line)
	}
	for _, want := range []string{"level=INFO", "outcome=allow", "action=job.run", "subject=root", "policyId="} {
		if !strings.Contains(line, want) {
			t.Errorf("allow log line missing %q; got:\n%s", want, line)
		}
	}
}

// TestDecisionLogDenyIsWarnWithCategory drives a policy deny and proves it logs at WARN with the
// generic category and reason attached — and still leaks no secret.
func TestDecisionLogDenyIsWarnWithCategory(t *testing.T) {
	handler := devLoginServer(t)
	log := captureLog(t)

	// dev-alice holds only L3-Consumer; job.run is ungranted → a policy deny.
	const secret = "another-secret-abc123"
	_, status := governWith(t, handler, "dev-alice",
		`{"action":"job.run","resource":"project/demo","goal":"try it","params":{"apiKey":"`+secret+`"}}`)
	if status != http.StatusForbidden {
		t.Fatalf("expected a deny (403), got %d", status)
	}

	line := log.String()
	if strings.Contains(line, secret) {
		t.Fatalf("SECURITY: a param value leaked into the decision log:\n%s", line)
	}
	for _, want := range []string{"level=WARN", "outcome=deny", "action=job.run", "subject=dev-alice", "category=action_not_allowed", "reason="} {
		if !strings.Contains(line, want) {
			t.Errorf("deny log line missing %q; got:\n%s", want, line)
		}
	}
}
