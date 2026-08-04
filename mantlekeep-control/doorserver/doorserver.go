// Package doorserver serves the one door over HTTP: the wire side of what the core
// already does as a library.
//
// It exists so the framework is self-sufficient. The SDKs ship a client that speaks
// this contract (Java's DoorClient in `service` mode is one), and until now nothing
// published implemented the other half — you could call a door, but not run one.
//
// The contract it serves is the frozen one in docs/door.md:
//
//	POST /api/govern   {action, resource, goal, env, params}
//	   allow            → {"outcome":"allow","token":…,"policyId":…,"expiresAt":…,"reasons":[]}
//	   deny             → {"outcome":"deny","reasons":[{"code":…,"message":…}],"policyId":…}
//	   require_approval → {"outcome":"require_approval","requiredApprovers":[…],"reasons":[…]}
//	GET  /api/audit    → {"intact":bool,"count":n,"records":[…]}
//	POST /api/login    {"user":…}  — DEV identity only, off by default
//
// It adds no governance of its own: every request is resolved to a subject and handed
// to the same Submitter the embedded path uses, so HTTP is a transport, never a second
// set of rules. A request with no resolvable caller is refused before the door is asked.
package doorserver

import (
	"encoding/json"
	"errors"
	"net/http"

	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/doorkit"
)

// Options configures a door server. Door is required; the rest are opt-in.
type Options struct {
	// Door is the assembled door this server fronts (see doorkit).
	Door *doorkit.Door

	// TrustedUserHeader names the header carrying the already-authenticated caller —
	// the production shape, where an SSO gateway or service mesh authenticates and the
	// door trusts what it asserts. Empty disables header identity.
	//
	// SECURITY: only set this when something in front of the door actually strips and
	// re-sets the header. Trusting a client-settable header is impersonation.
	TrustedUserHeader string

	// DelegatedSubjectHeader names the header by which an authenticated SERVICE says
	// which person it is acting for — the business-to-business case, where a service
	// account authenticates but a human is the one whose action this really is.
	//
	// Both identities are recorded: the person as the SUBJECT (who acted) and the
	// service as VIA (which application carried the claim). An audit that keeps only
	// one of them cannot answer the question it exists to answer.
	//
	// Empty disables delegation entirely.
	DelegatedSubjectHeader string

	// Delegators lists the authenticated callers permitted to act for someone else.
	//
	// SECURITY — this list is the whole control. Without it, any caller could name any
	// subject and the door would believe it, which is impersonation with an audit trail
	// that lies. A caller NOT on this list attempting delegation is REFUSED; it is never
	// silently downgraded to acting as itself, because that would turn a privilege
	// violation into a successful request.
	Delegators []string

	// DevLogin enables POST /api/login, which mints a cookie session for a named user
	// with NO credential check. Development and tests only — never enable it where a
	// real identity source exists.
	DevLogin bool
}

// Server serves the door's HTTP contract. Build it with New and mount it anywhere an
// http.Handler goes, so a product can serve the door alongside its own routes.
type Server struct {
	door     *doorkit.Door
	sessions *sessionStore
	options  Options
}

// New builds the door server. It fails fast when misconfigured, because a door that
// starts without a way to identify callers would deny everything at runtime instead.
func New(options Options) (*Server, error) {
	if options.Door == nil {
		return nil, errMissingDoor
	}
	if options.TrustedUserHeader == "" && !options.DevLogin {
		return nil, errNoIdentitySource
	}
	return &Server{door: options.Door, sessions: newSessionStore(), options: options}, nil
}

// Handler returns the routes: the door, the audit view, and (when enabled) dev login.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/govern", s.handleGovern)
	mux.HandleFunc("GET /api/audit", s.handleAudit)
	if s.options.DevLogin {
		mux.HandleFunc("POST /api/login", s.handleDevLogin)
	}
	return mux
}

// governRequest is the wire shape. These names are the frozen contract — renaming one
// breaks every SDK client and requires a MAJOR contract bump (docs/door.md).
type governRequest struct {
	Action   string         `json:"action"`
	Resource string         `json:"resource"`
	Goal     string         `json:"goal"`
	Env      string         `json:"env"`
	Params   map[string]any `json:"params"`
}

func (s *Server) handleGovern(writer http.ResponseWriter, request *http.Request) {
	actor, err := s.resolveCaller(request)
	if err != nil {
		// No accountable actor means nothing to record and nothing to authorise. This is
		// an identity refusal (401); an overreaching delegator is refused outright (403)
		// rather than downgraded to acting as itself.
		status := http.StatusUnauthorized
		if errors.Is(err, errDelegationRefused) {
			status = http.StatusForbidden
		}
		writeJSON(writer, status, map[string]any{
			"outcome": "deny",
			"reasons": []wireReason{{Code: codeIdentity, Message: err.Error()}},
		})
		return
	}

	var body governRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{
			"outcome": "deny",
			"reasons": []wireReason{{Code: codeValidation, Message: "malformed request: " + err.Error()}},
		})
		return
	}

	params := body.Params
	if params == nil {
		params = map[string]any{}
	}
	// env travels in params: the engine names no environment, it only reads the key a
	// product's floor DATA refers to.
	if body.Env != "" {
		params["env"] = body.Env
	}

	intentID := newIntentID()
	token, submitErr := s.door.Submitter.Submit(request.Context(), mantlekeep.Intent{
		ID:      intentID,
		Subject: actor.subject,
		// Which application carried the claim. Empty when the subject acted itself.
		Via:      actor.via,
		Action:   body.Action,
		Resource: body.Resource,
		Spec:     mantlekeep.IntentSpec{Goal: body.Goal},
		Params:   params,
	})
	if submitErr != nil {
		// A deny or require_approval is a normal, recorded outcome carried as a
		// DecisionError — the wire keeps the policy id, the typed reason, and who may
		// sign off. Any other error is a genuine server fault (policy eval, audit write)
		// and is the only thing that becomes a 500.
		var decisionErr *mantlekeep.DecisionError
		if errors.As(submitErr, &decisionErr) {
			decision := decisionErr.Decision
			decisionLog{
				outcome:  decision.Action,
				action:   body.Action,
				subject:  actor.subject.ID,
				via:      actor.via,
				policyID: decision.PolicyID,
				category: decision.Category,
				reason:   decision.Reason,
			}.emit()
			writeDecision(writer, decision, intentID)
			return
		}
		writeJSON(writer, http.StatusInternalServerError, map[string]any{
			"outcome": "error",
			"reasons": []wireReason{{Code: codePolicyError, Message: submitErr.Error()}},
		})
		return
	}

	decisionLog{
		outcome:  mantlekeep.ActionAllow,
		action:   body.Action,
		subject:  actor.subject.ID,
		via:      actor.via,
		policyID: token.PolicyID,
	}.emit()
	writeAllow(writer, token)
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
