package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// A read of another team's estate must be governed, not merely authenticated.
//
// # Why these tests exist
//
// GET /api/estate/{team} resolved the caller and then discarded it, using the team from the URL
// unchecked. Any name in X-Caller read any team: its manifest, and its drift report naming which
// approved resources do not yet exist. Demonstrated against the real handler over real HTTP before
// this was written — alice, who owns payments, read treasury and received its Kafka topics and
// every "approved but absent" gap in it.
//
// The handler said so itself: "No intent is submitted, because nothing happens as a result of a
// read and there is no decision to record." That holds for mutation and fails for disclosure.
// Nothing HAPPENS on a read; something is REVEALED, and who may see it is exactly the kind of
// question the door exists to answer.
//
// The api package had no tests at all, which is how this shipped.

// recordingDoor is a door that remembers what it was asked and answers as told.
//
// A stub rather than the real doorserver on purpose: what is under test is whether the handler
// ASKS, and an answer the test controls is the only way to assert both outcomes. The wire contract
// between this client and the real door is proven separately, by doorclient's round-trip tests
// against the real doorserver.
type recordingDoor struct {
	asked  []mantlekeep.Intent
	refuse error
}

func (d *recordingDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.asked = append(d.asked, intent)
	if d.refuse != nil {
		return mantlekeep.ExecutionToken{}, d.refuse
	}
	return mantlekeep.ExecutionToken{Value: "token", IntentID: intent.ID,
		ExpiresAt: time.Now().Add(time.Minute)}, nil
}

// twoTeams stands up the real handler over real HTTP with two teams declared.
func twoTeams(t *testing.T, door *recordingDoor) *httptest.Server {
	t.Helper()

	floor := estate.DefaultFloor()
	store := estate.NewMemoryManifests()
	for _, spec := range []string{
		`{"team":"payments","owns":"payments","tier":"prod","kafka":{"topics":["settlements"]}}`,
		`{"team":"treasury","owns":"treasury","tier":"prod","kafka":{"topics":["fx-positions"]}}`,
	} {
		manifest, err := estate.ParseManifest([]byte(spec))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := store.Remember(context.Background(), manifest); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}

	service := estate.NewService(floor, store)
	manager := estate.NewManager(door, floor).
		RememberManifestsIn(store).
		ReadFootprintsFrom(service)

	mux := http.NewServeMux()
	api.New(manager, service, api.HeaderCallers{}).Routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func readAs(t *testing.T, server *httptest.Server, caller, team string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/estate/"+team, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if caller != "" {
		request.Header.Set(api.UserHeader, caller)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	return response.StatusCode, string(body)
}

// The read must reach the door at all. Everything else here depends on it.
func TestAReadSubmitsAnIntent(t *testing.T) {
	door := &recordingDoor{}
	server := twoTeams(t, door)

	if status, _ := readAs(t, server, "dev-alice", "treasury"); status != http.StatusOK {
		t.Fatalf("an allowed read must succeed; got %d", status)
	}

	if len(door.asked) == 0 {
		t.Fatal("the door was never asked: a read that submits no intent is authenticated and " +
			"ungoverned, so the team in the URL is never compared to anything")
	}
	intent := door.asked[0]
	if intent.Action != "estate.read" {
		t.Errorf("the read must submit its own action, or a policy cannot grant reading "+
			"separately from writing; got %q", intent.Action)
	}
	if intent.Resource != "team/treasury" {
		t.Errorf("the intent must name the team being READ, or the door rules on the wrong "+
			"scope; got %q", intent.Resource)
	}
	if intent.Subject.ID != "dev-alice" {
		t.Errorf("the intent must carry the caller, or the door decides about nobody; got %q",
			intent.Subject.ID)
	}
}

// A refusal must not disclose the thing it refused.
//
// 404 rather than 403 deliberately: a 403 confirms the team exists, and for an endpoint whose
// whole risk is disclosure, the existence of another team's estate is itself worth withholding.
func TestARefusedReadDisclosesNothing(t *testing.T) {
	door := &recordingDoor{refuse: errors.New("deny: no role permits action estate.read")}
	server := twoTeams(t, door)

	status, body := readAs(t, server, "dev-alice", "treasury")
	if status != http.StatusNotFound {
		t.Errorf("a refused read must answer 404, not %d — a 403 confirms the team exists, "+
			"which is the disclosure being withheld", status)
	}
	if strings.Contains(body, "fx-positions") {
		t.Error("the refused response carried the team's topic names: the body is the disclosure, " +
			"and a status code in front of it withholds nothing")
	}
	if strings.Contains(body, "approved but absent") {
		t.Error("the refused response carried the team's drift report, which names the approved " +
			"resources that do not exist — reconnaissance, not merely disclosure")
	}
}

// Authentication is not authorization, stated as a test.
//
// Both callers are authenticated. Neither owns the other's team. The door decides, and it must be
// ASKED about each one separately — a handler that asks once and caches, or asks about the caller
// without naming the team, passes the test above and fails this one.
func TestEachTeamIsDecidedSeparately(t *testing.T) {
	door := &recordingDoor{}
	server := twoTeams(t, door)

	readAs(t, server, "dev-alice", "payments")
	readAs(t, server, "dev-alice", "treasury")

	if len(door.asked) != 2 {
		t.Fatalf("two reads of two teams must produce two decisions; got %d", len(door.asked))
	}
	if door.asked[0].Resource == door.asked[1].Resource {
		t.Errorf("both intents named %q: the team in the URL is not reaching the door, so one "+
			"decision is standing in for every team", door.asked[0].Resource)
	}
}

// No identity is still 401, and must not reach the door.
//
// Failing closed BEFORE the door matters: an unauthenticated request that reaches it arrives with
// an empty subject, and a decision recorded about nobody is a record that cannot be read back.
func TestAnUnidentifiedReadNeverReachesTheDoor(t *testing.T) {
	door := &recordingDoor{}
	server := twoTeams(t, door)

	if status, _ := readAs(t, server, "", "treasury"); status != http.StatusUnauthorized {
		t.Errorf("a request with no identity must be refused with 401; got %d", status)
	}
	if len(door.asked) != 0 {
		t.Errorf("an unidentified request reached the door, which would record a decision about "+
			"an empty subject; %d intent(s) submitted", len(door.asked))
	}
}
