package policybundle

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

func handlerFor(source grants.Loader) *Handler {
	return NewHandler(func() (Bundle, error) {
		return Build(context.Background(), source)
	}, slog.New(slog.DiscardHandler))
}

// A poll that finds nothing new must cost nothing. Most polls find nothing new, and a gateway
// re-downloading unchanged policy every few seconds is how a control plane becomes the reason
// the gateway is slow.
func TestAPollThatFindsNothingNewIsAnswered304(t *testing.T) {
	handler := handlerFor(policy("deploy.dev"))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/policy/bundle.tar.gz", nil))
	if first.Code != http.StatusOK || first.Body.Len() == 0 {
		t.Fatalf("first fetch = %d with %d bytes", first.Code, first.Body.Len())
	}
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag — every poll would re-download the whole policy")
	}

	for _, presented := range []string{tag, "W/" + tag, `"other", ` + tag} {
		again := httptest.NewRequest(http.MethodGet, "/policy/bundle.tar.gz", nil)
		again.Header.Set("If-None-Match", presented)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, again)
		if recorder.Code != http.StatusNotModified {
			t.Fatalf("If-None-Match %q answered %d, want 304", presented, recorder.Code)
		}
		if recorder.Body.Len() != 0 {
			t.Fatalf("a 304 carried %d bytes", recorder.Body.Len())
		}
	}
}

// The revision must be readable without decoding cache semantics: an operator diagnosing a
// split brain curls this and compares it to what the door reports.
func TestTheRevisionIsReadableAsAPlainHeader(t *testing.T) {
	source := policy("deploy.dev")
	_, _, expected, _ := source.Load(context.Background())
	recorder := httptest.NewRecorder()
	handlerFor(source).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/b", nil))
	if got := recorder.Header().Get("X-Policy-Revision"); got != string(expected) {
		t.Fatalf("X-Policy-Revision = %q, want %q", got, expected)
	}
}

// An unreachable source must fail the REQUEST. An OPA that receives an error keeps the policy it
// already has; one that receives an empty bundle installs a deny-all and takes the gateway down.
func TestAnUnreachableSourceFailsTheRequestRatherThanServingAnEmptyPolicy(t *testing.T) {
	handler := NewHandler(func() (Bundle, error) {
		return Bundle{}, errors.New("the policy store is unreachable")
	}, slog.New(slog.DiscardHandler))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/b", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreachable source answered %d, want 503", recorder.Code)
	}
	if recorder.Header().Get("ETag") != "" {
		t.Fatal("a failed build still published an ETag — a caller could cache the failure")
	}
}

// The bundle endpoint is read-only. It publishes what the door decided; it is not a second way
// to change policy.
func TestTheBundleEndpointRefusesWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	handlerFor(policy("deploy.dev")).ServeHTTP(recorder,
		httptest.NewRequest(http.MethodPost, "/b", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST answered %d, want 405", recorder.Code)
	}
}
