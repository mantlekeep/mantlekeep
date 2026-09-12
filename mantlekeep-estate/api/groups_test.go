package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// The gateway's asserted groups must survive the estate and reach the door.
//
// # Why these tests exist
//
// On the SSO tier the door resolves roles from a group→role table and refuses an identity whose
// groups map to nothing. The estate's caller resolver returned Subject{ID: name} and dropped the
// groups, so a door configured that way could resolve NOBODY through this service — and the
// refusal landed at identity, before policy, so the door recorded nothing at all.
//
// Groups are not roles. A caller may say which groups it is in; it may never say what they are
// worth. That is the whole reason forwarding them is safe, and the reason Roles are still never
// forwarded.

// resolveFailed is the one message for a caller that would not resolve. Named because the same
// sentence is asserted in every case here, and four copies of a format string are four places to
// edit when the wording changes.
const resolveFailed = "Caller: %v"

func TestTheCallerCarriesTheGatewaysGroups(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/estate/payments", nil)
	request.Header.Set(api.UserHeader, "reader-rachel")
	request.Header.Set(api.GroupsHeader, "directory-payments-readers,directory-all-staff")

	subject, err := api.HeaderCallers{}.Caller(request)
	if err != nil {
		t.Fatalf(resolveFailed, err)
	}
	if len(subject.ADGroups) != 2 {
		t.Fatalf("the gateway asserted 2 groups and %d survived: a door that resolves roles "+
			"from groups can resolve nobody through this service", len(subject.ADGroups))
	}
	if subject.ADGroups[0] != "directory-payments-readers" {
		t.Errorf("first group = %q", subject.ADGroups[0])
	}
}

// Whitespace and empty entries must not become groups.
//
// An empty group name matches nothing in a group→role table, so carrying one would put a group
// into the subject that exists and is unmapped — noise in the decision record, and a value a
// later table could accidentally bind.
func TestBlankGroupEntriesAreDropped(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/estate/payments", nil)
	request.Header.Set(api.UserHeader, "reader-rachel")
	request.Header.Set(api.GroupsHeader, " directory-payments-readers , , ,  ")

	subject, err := api.HeaderCallers{}.Caller(request)
	if err != nil {
		t.Fatalf(resolveFailed, err)
	}
	if len(subject.ADGroups) != 1 || subject.ADGroups[0] != "directory-payments-readers" {
		t.Errorf("expected exactly one trimmed group, got %q", subject.ADGroups)
	}
}

// No groups header is still a valid caller — the dev tier resolves roles from the id.
func TestACallerWithNoGroupsHeaderIsStillACaller(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/estate/payments", nil)
	request.Header.Set(api.UserHeader, "dev-alice")

	subject, err := api.HeaderCallers{}.Caller(request)
	if err != nil {
		t.Fatalf(resolveFailed, err)
	}
	if subject.ID != "dev-alice" {
		t.Errorf("id = %q", subject.ID)
	}
	if len(subject.ADGroups) != 0 {
		t.Errorf("no groups were asserted, so none must be invented; got %q", subject.ADGroups)
	}
}

// Roles are still never carried from the request.
//
// The header tier can only ever say who you are and where you belong. If a caller could assert a
// role, it could assert its way past any gate — so this stays true however many identity headers
// are added.
func TestRolesAreNeverTakenFromTheRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/estate/payments", nil)
	request.Header.Set(api.UserHeader, "reader-rachel")
	request.Header.Set(api.GroupsHeader, "directory-payments-readers")
	// A caller trying its luck.
	request.Header.Set("X-Caller-Roles", "L0-SuperAdmin")

	subject, err := api.HeaderCallers{}.Caller(request)
	if err != nil {
		t.Fatalf(resolveFailed, err)
	}
	if len(subject.Roles) != 0 {
		t.Errorf("a role reached the subject from the request: %q", subject.Roles)
	}
}
