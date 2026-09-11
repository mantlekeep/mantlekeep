package doorserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/identity"
)

// A gateway that authenticates by GROUP must be able to say which groups.
//
// # Why these tests exist
//
// The SSO tier resolves roles from the IdP's groups: the gateway resolver is built from a
// group→role table (MANTLEKEEP_GROUP_ROLES) and refuses an identity whose groups map to nothing.
// But resolveUser called Resolve with ExternalIdentity{ID: userID} and NOTHING else, and the
// submit path then overwrote the intent's subject with that result. So over HTTP there was no
// path by which a group could reach the resolver at all.
//
// The effect, proven by running the bank's own shape end to end before this was written: with
// MANTLEKEEP_AUTH=proxy every request failed — and failed at IDENTITY, before any policy
// decision, so the door logged nothing. An operator sees "403" and goes looking for a policy bug
// that does not exist.
//
// The header is trusted on exactly the same terms as TrustedUserHeader: something in front must
// authenticate and strip any client-supplied copy. It carries GROUPS, never roles — the resolver
// remains the authority on what a group means, so a caller still cannot assert its own authority.

// groupsOnly is the SSO-tier resolver: it knows groups, and refuses an id it cannot map.
func groupsOnly() mantlekeep.IdentityResolver {
	return identity.NewGateway(map[string][]mantlekeep.Role{
		"InfoDir-FRAME-MANTLE-PAYMENTS-Reader":   {mantlekeep.RoleConsumer},
		"InfoDir-FRAME-MANTLE-PAYMENTS-Operator": {mantlekeep.RoleOperator},
	})
}

// serveWithGroups builds a door server whose identity comes from a gateway's groups.
func serveWithGroups(t *testing.T, groupsHeader string) *httptest.Server {
	t.Helper()

	door := newTestDoor(t)
	door.Identity = groupsOnly()

	server, err := New(Options{
		Door:                door,
		TrustedUserHeader:   "X-Caller",
		TrustedGroupsHeader: groupsHeader,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

// submit posts an intent and reports the status and body.
func submit(t *testing.T, server *httptest.Server, caller, groupsHeader, groups string) (int, string) {
	t.Helper()

	intent := mantlekeep.Intent{
		ID: "GRP-1", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: caller},
		Spec:    mantlekeep.IntentSpec{Goal: "prove a group reaches the resolver"},
	}
	body, err := json.Marshal(intent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/govern", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Caller", caller)
	if groupsHeader != "" && groups != "" {
		request.Header.Set(groupsHeader, groups)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return response.StatusCode, string(answer)
}

// The groups a gateway asserts must reach the resolver.
func TestTheGroupsHeaderReachesTheResolver(t *testing.T) {
	server := serveWithGroups(t, "X-Caller-Groups")

	status, body := submit(t, server, "reader-rachel", "X-Caller-Groups",
		"InfoDir-FRAME-MANTLE-PAYMENTS-Reader")

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		t.Fatalf("identity failed for a caller whose group maps to a role: %d %s\n"+
			"the gateway resolver never saw the group, so nobody can be resolved over HTTP at all",
			status, strings.TrimSpace(body))
	}
}

// Without the header, the same caller must still be refused.
//
// The negative case matters as much as the positive one: if identity succeeded with no groups
// supplied, the resolver would be inventing a population, and the header would be decoration.
func TestACallerWithNoGroupsIsStillRefused(t *testing.T) {
	server := serveWithGroups(t, "X-Caller-Groups")

	status, _ := submit(t, server, "reader-rachel", "", "")
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Errorf("a caller whose groups are unknown must be refused, got %d — a resolver that "+
			"answers without groups is deciding authority from an id alone", status)
	}
}

// A group a deployment has not mapped grants nothing.
func TestAnUnmappedGroupGrantsNothing(t *testing.T) {
	server := serveWithGroups(t, "X-Caller-Groups")

	status, _ := submit(t, server, "reader-rachel", "X-Caller-Groups", "SomeOther-AD-Group")
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Errorf("an unmapped group must grant nothing, got %d", status)
	}
}

// The groups header may not name a credential, for the same reason the user header may not.
//
// A credential in this header would be read as the caller's GROUPS and land in the decision
// record — in an append-only chain, where it cannot be redacted without breaking it.
func TestNewRefusesACredentialHeaderAsTheGroupsHeader(t *testing.T) {
	for _, name := range []string{"Authorization", "cookie", "X-Api-Key", "  Authorization  "} {
		door := newTestDoor(t)
		_, err := New(Options{
			Door:                door,
			TrustedUserHeader:   "X-Caller",
			TrustedGroupsHeader: name,
		})
		if err == nil {
			t.Errorf("TrustedGroupsHeader=%q was accepted: a credential would be recorded as "+
				"the caller's groups in the audit chain", name)
		}
	}
}
