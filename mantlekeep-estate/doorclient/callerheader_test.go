package doorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// trustedHeaderDoor stands in for a doorserver configured with a TrustedUserHeader: it
// identifies the caller from ONE named header and from nothing else, and refuses a request
// without it the way the real door does.
//
// A stub that accepts anything cannot tell a present header from an absent one, which is why
// the client sending its service account only as `Authorization: Bearer` — a header the door
// never reads — went unnoticed on both sides.
func trustedHeaderDoor(t *testing.T, header string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := strings.TrimSpace(r.Header.Get(header))
		seen = append(seen, caller)
		if caller == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(governResponse{Outcome: "deny",
				Reasons: []wireReason{{Code: "unauthenticated", Message: "no caller identity"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(governResponse{Outcome: "allow", Token: "tok-1"})
	}))
	t.Cleanup(door.Close)
	return door, &seen
}

// Without the option the client presents no identity the door reads. Pinned so the default
// cannot drift without someone noticing: an empty caller header must stay an explicit choice.
func TestWithoutACallerHeaderTheDoorSeesNobody(t *testing.T) {
	door, seen := trustedHeaderDoor(t, "X-Caller")

	_, err := New(door.URL, "platform-estate").Submit(context.Background(), intent())
	if err == nil {
		t.Fatal("a door that identifies callers by header allowed a client that sends none")
	}
	if !strings.Contains(err.Error(), "no caller identity") {
		t.Errorf("got %v, want the door's unauthenticated refusal", err)
	}
	if len(*seen) != 1 || (*seen)[0] != "" {
		t.Errorf("the door saw %q in its trusted header, want it empty", *seen)
	}
}

// With the option the service account arrives in the header the door actually reads.
func TestTheCallerHeaderCarriesTheServiceAccount(t *testing.T) {
	door, seen := trustedHeaderDoor(t, "X-Caller")
	client, err := NewWithOptions(door.URL, "platform-estate", WithCallerHeader("X-Caller"))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	token, err := client.Submit(context.Background(), intent())
	if err != nil {
		t.Fatalf("the door refused an identified caller: %v", err)
	}
	if token.Value == "" {
		t.Error("an allow carried no token")
	}
	if len(*seen) != 1 || (*seen)[0] != "platform-estate" {
		t.Errorf("the door saw %q, want the service account", *seen)
	}
}

// Who the client IS and who it acts FOR travel in different headers, and both must arrive. The
// door records caller{subject: user, via: account}; pointing its trusted header at the
// on-behalf-of header would make a write succeed with the delegation erased from the record.
func TestTheCallerAndTheSubjectAreDifferentHeaders(t *testing.T) {
	var gotCaller, gotOnBehalfOf string
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCaller = r.Header.Get("X-Caller")
		gotOnBehalfOf = r.Header.Get(onBehalfOfHeader)
		_ = json.NewEncoder(w).Encode(governResponse{Outcome: "allow", Token: "tok-1"})
	}))
	defer door.Close()

	client, err := NewWithOptions(door.URL, "platform-estate", WithCallerHeader("X-Caller"))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := client.Submit(context.Background(), intent()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if gotCaller != "platform-estate" {
		t.Errorf("caller header = %q, want the service account", gotCaller)
	}
	if gotOnBehalfOf != "dev-alice" {
		t.Errorf("%s = %q, want the human", onBehalfOfHeader, gotOnBehalfOf)
	}
}

// A credential header is refused at construction, for the same reason doorserver refuses it:
// its value would be written to an append-only chain as the caller's id and could never be
// taken back out.
func TestACredentialHeaderIsRefusedAsTheCallerHeader(t *testing.T) {
	for _, name := range []string{"Authorization", "authorization", " Cookie ", "X-Api-Key"} {
		_, err := NewWithOptions("http://door.invalid", "platform-estate", WithCallerHeader(name))
		if err == nil {
			t.Errorf("WithCallerHeader(%q) was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "CREDENTIAL") {
			t.Errorf("WithCallerHeader(%q): %v, want a credential-header refusal", name, err)
		}
	}
}
