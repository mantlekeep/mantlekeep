package doorserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
)

func newTestDoor(t *testing.T) *doorkit.Door {
	t.Helper()
	door, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewInMemoryDoor: %v", err)
	}
	return door
}

// The door records the caller's id in the hash-chained audit trail. If the identity
// header named a credential, the door would record a bearer token as a user id — into an
// append-only, tamper-evident log where it cannot be redacted afterwards without breaking
// the chain. So the misconfiguration is refused at construction, not documented.

func TestNewRefusesACredentialHeaderAsTheIdentityHeader(t *testing.T) {
	door := newTestDoor(t)

	credentialHeaders := []string{
		"Authorization",
		"authorization", // header names are case-insensitive
		"AUTHORIZATION",
		"Proxy-Authorization",
		"Cookie",
		"Set-Cookie",
		"X-Api-Key",
		"Api-Key",
		"X-Auth-Token",
		"  Authorization  ", // surrounding whitespace must not smuggle it past
	}

	for _, header := range credentialHeaders {
		t.Run("trusted/"+header, func(t *testing.T) {
			_, err := New(Options{Door: door, TrustedUserHeader: header})
			if err == nil {
				t.Fatalf("New accepted %q as TrustedUserHeader — a credential would be written to the audit chain", header)
			}
			if !strings.Contains(err.Error(), "CREDENTIAL") {
				t.Errorf("the refusal does not explain itself: %v", err)
			}
		})

		t.Run("delegated/"+header, func(t *testing.T) {
			_, err := New(Options{
				Door:                   door,
				TrustedUserHeader:      "X-Caller",
				DelegatedSubjectHeader: header,
			})
			if err == nil {
				t.Fatalf("New accepted %q as DelegatedSubjectHeader", header)
			}
		})
	}
}

func TestNewAcceptsOrdinaryIdentityHeaders(t *testing.T) {
	door := newTestDoor(t)

	for _, header := range []string{"X-Caller", "X-Forwarded-User", "X-Auth-Request-User", "Remote-User"} {
		if _, err := New(Options{Door: door, TrustedUserHeader: header}); err != nil {
			t.Errorf("New refused the ordinary identity header %q: %v", header, err)
		}
	}

	// An empty delegated header means delegation is disabled, which is not a credential.
	if _, err := New(Options{Door: door, TrustedUserHeader: "X-Caller", DelegatedSubjectHeader: ""}); err != nil {
		t.Errorf("New refused an empty DelegatedSubjectHeader: %v", err)
	}
}
