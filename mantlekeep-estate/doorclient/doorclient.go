// Package doorclient submits intents to a MantleKeep door over HTTP.
//
// It is a [mantlekeep.Submitter], so the decision layer cannot tell an in-process door from a
// remote one — which is the point of composing binaries over a versioned wire contract rather
// than by embedding. The grant control plane and the door are separately deployable, and the
// only thing that crosses between them is a decision.
//
// Stdlib only. The door's contract is a JSON POST; a client library for it would be a
// dependency in every product that governs anything.
package doorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// onBehalfOfHeader carries the subject this application is acting for. A HEADER rather than a
// body field, matching the door's own contract: the intent describes WHAT is asked, and who is
// asking stays a property of the call. An actor in the payload is something any caller can claim.
const onBehalfOfHeader = "X-On-Behalf-Of"

// Client is the door, reached over HTTP.
type Client struct {
	baseURL string
	// serviceAccount is THIS application's principal at the door — the credential that makes it
	// a trusted delegator. It is never the human: the door authenticates an application, and
	// that application says who it acts for.
	serviceAccount string
	http           *http.Client
}

var _ mantlekeep.Submitter = (*Client)(nil)

// New builds a client for the door at baseURL (e.g. "http://localhost:8080"), presenting
// serviceAccount as its own principal.
func New(baseURL, serviceAccount string) *Client {
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		serviceAccount: serviceAccount,
		// A bounded timeout, because a door that hangs must not hang every apply behind it.
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// governRequest is the door's published request shape.
type governRequest struct {
	// ID is OUR identifier for this change, and the door records it rather than synthesising
	// one. Without it the door built the same id from subject and action for every request, so
	// two applies by one person were one string on the chain and neither could be found again.
	ID       string         `json:"id,omitempty"`
	Action   string         `json:"action"`
	Resource string         `json:"resource"`
	Goal     string         `json:"goal"`
	Scope    string         `json:"scope,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

// governResponse is the door's published reply, for allow and refusal alike.
type governResponse struct {
	Decision string `json:"decision"`
	// IntentID is what the chain actually holds. Echoed back rather than assumed, because a
	// caller that guesses at the door's id scheme is one door change away from citing a record
	// that does not exist.
	IntentID string `json:"intentId"`
	Token    string `json:"token"`
	Expires  string `json:"expires"`
	Reason   string `json:"reason"`
	// RequiredApprovers is who may sign off, when the decision was require_approval. Carried
	// because a refusal that cannot say who unblocks it is a dead end.
	RequiredApprovers []string `json:"requiredApprovers,omitempty"`
}

// Submit sends one intent to the door and returns the token it issued.
//
// A REFUSAL comes back as an error in the door's own words, formatted exactly as an in-process
// door formats it — "<decision>: <reason>". That symmetry is load-bearing: everything upstream
// classifies refusals by that shape, and a remote door that phrased its refusals differently
// would need a second classifier that could drift from the first.
//
// A TRANSPORT failure is deliberately worded so it can never be mistaken for a decision. The
// door did not refuse; the door did not answer. Reporting the second as the first would record
// a network fault as a governance outcome and tell a team their change was denied when nobody
// ever ruled on it.
func (c *Client) Submit(ctx context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	scope, _ := intent.Params["scope"].(string)
	body, err := json.Marshal(governRequest{
		ID:     intent.ID,
		Action: intent.Action, Resource: intent.Resource, Goal: intent.Spec.Goal,
		Scope: scope, Params: intent.Params,
	})
	if err != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("door: encoding intent %s: %w", intent.ID, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/govern",
		bytes.NewReader(body))
	if err != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("door: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.serviceAccount)
	if intent.Subject.ID != "" {
		request.Header.Set(onBehalfOfHeader, intent.Subject.ID)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf(
			"door unreachable: %w — no decision was made, so nothing may be treated as approved "+
				"or refused", err)
	}
	defer response.Body.Close()

	var decoded governResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf(
			"door unreadable: HTTP %d with an undecodable body: %w", response.StatusCode, err)
	}

	if response.StatusCode != http.StatusOK || decoded.Decision != string(mantlekeep.ActionAllow) {
		return mantlekeep.ExecutionToken{}, refusal(decoded, response.StatusCode)
	}
	if decoded.Token == "" {
		// An allow with no token is not an allow. Handing an adapter an empty token would let
		// it act with nothing behind it, and every adapter in the estate refuses one anyway.
		return mantlekeep.ExecutionToken{}, fmt.Errorf(
			"door: allowed intent %s but issued no execution token", intent.ID)
	}

	// The id the CHAIN holds, not the one we sent. They are the same whenever the door honours
	// our id — but reporting what we asked for rather than what was recorded is how a caller
	// ends up citing a record that does not exist.
	recorded := decoded.IntentID
	if recorded == "" {
		recorded = intent.ID
	}
	token := mantlekeep.ExecutionToken{
		Value: decoded.Token, IntentID: recorded, Scope: intent.Resource, IssuedAt: time.Now().UTC(),
	}
	if expires, err := time.Parse(time.RFC3339, decoded.Expires); err == nil {
		token.ExpiresAt = expires
	}
	return token, nil
}

// refusal renders the door's verdict in the door's own words.
func refusal(decoded governResponse, status int) error {
	decision := decoded.Decision
	if decision == "" {
		// The door answered but named no decision — that is a broken contract, not a refusal,
		// and must not be classified as one upstream.
		return fmt.Errorf("door answered HTTP %d with no decision: %s", status, decoded.Reason)
	}
	reason := decoded.Reason
	if reason == "" {
		reason = "no reason given"
	}
	// TYPED, so a caller can tell a change awaiting a person from one that is forbidden without
	// re-parsing this message. The message itself is unchanged — everything upstream that only
	// reads the string keeps working.
	return &mantlekeep.Refused{
		Action:            mantlekeep.DecisionAction(decision),
		Reason:            reason,
		RequiredApprovers: rolesFrom(decoded.RequiredApprovers),
	}
}

// rolesFrom converts the door's role names into the core's Role type.
func rolesFrom(names []string) []mantlekeep.Role {
	if len(names) == 0 {
		return nil
	}
	roles := make([]mantlekeep.Role, 0, len(names))
	for _, name := range names {
		roles = append(roles, mantlekeep.Role(name))
	}
	return roles
}
