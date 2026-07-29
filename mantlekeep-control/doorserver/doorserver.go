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
//	                   → {"decision":"allow","token":…} | {"decision":"deny","reason":…}
//	GET  /api/audit    → {"intact":bool,"count":n,"records":[…]}
//	POST /api/login    {"user":…}  — DEV identity only, off by default
//
// It adds no governance of its own: every request is resolved to a subject and handed
// to the same Submitter the embedded path uses, so HTTP is a transport, never a second
// set of rules. A request with no resolvable caller is refused before the door is asked.
package doorserver

import (
	"encoding/json"
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
	subject, ok := s.resolveCaller(request)
	if !ok {
		// No identity means no accountable actor, so there is nothing to record and
		// nothing to authorise. Refuse before the door is troubled.
		writeJSON(writer, http.StatusUnauthorized,
			map[string]any{"decision": "deny", "reason": "unauthenticated: no caller identity"})
		return
	}

	var body governRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		writeJSON(writer, http.StatusBadRequest,
			map[string]any{"decision": "deny", "reason": "malformed request: " + err.Error()})
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

	token, err := s.door.Submitter.Submit(request.Context(), mantlekeep.Intent{
		ID:       newIntentID(),
		Subject:  subject,
		Action:   body.Action,
		Resource: body.Resource,
		Spec:     mantlekeep.IntentSpec{Goal: body.Goal},
		Params:   params,
	})
	if err != nil {
		// A deny is a normal, recorded outcome — not a server fault. 403 says the door
		// answered and the answer was no.
		writeJSON(writer, http.StatusForbidden,
			map[string]any{"decision": "deny", "reason": err.Error()})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"decision": "allow",
		"token":    token.Value,
		"intentId": token.IntentID,
	})
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
