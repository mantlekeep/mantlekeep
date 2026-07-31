package doorserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	mantlekeep "mantlekeep.dev/control"
)

var (
	errMissingDoor      = errors.New("doorserver: Options.Door is required")
	errUnauthenticated  = errors.New("unauthenticated: no caller identity")
	errNoIdentitySource = errors.New(
		"doorserver: no way to identify callers — set TrustedUserHeader (production) or DevLogin (dev only)")
)

const sessionCookieName = "mantlekeep_session"

// caller is the outcome of identifying a request: the subject whose action this is,
// and — when a service acted for someone else — which service carried the claim.
type caller struct {
	subject mantlekeep.Subject
	via     string // the authenticated delegator; empty when the subject acted itself
}

// errDelegationRefused reports a caller that named a subject it is not permitted to
// act for. It is deliberately distinct from "unauthenticated": the caller IS known,
// and is overreaching.
var errDelegationRefused = errors.New("caller is not permitted to act for another subject")

// resolveCaller determines WHO is acting and, separately, WHO ASSERTED it.
//
// The authenticated caller is established first. If it also names a delegated subject,
// that is only honoured when the caller is a permitted delegator — otherwise the
// request is refused outright rather than quietly downgraded, since silently ignoring
// an attempted impersonation would turn a privilege violation into a success.
func (s *Server) resolveCaller(request *http.Request) (caller, error) {
	authenticated, ok := s.authenticatedCaller(request)
	if !ok {
		return caller{}, errUnauthenticated
	}

	delegatedID := ""
	if header := s.options.DelegatedSubjectHeader; header != "" {
		delegatedID = strings.TrimSpace(request.Header.Get(header))
	}
	if delegatedID == "" || delegatedID == authenticated {
		subject, resolved := s.resolveUser(request, authenticated)
		if !resolved {
			return caller{}, errUnauthenticated
		}
		return caller{subject: subject}, nil
	}

	if !s.isDelegator(authenticated) {
		return caller{}, errDelegationRefused
	}
	subject, resolved := s.resolveUser(request, delegatedID)
	if !resolved {
		// The delegator is trusted, but the person it named is unknown — refuse rather
		// than record an action against an identity the directory cannot confirm.
		return caller{}, errUnauthenticated
	}
	return caller{subject: subject, via: authenticated}, nil
}

// authenticatedCaller returns the id of the caller that authenticated, by trusted
// header or by dev session.
func (s *Server) authenticatedCaller(request *http.Request) (string, bool) {
	if header := s.options.TrustedUserHeader; header != "" {
		if id := strings.TrimSpace(request.Header.Get(header)); id != "" {
			return id, true
		}
	}
	if s.options.DevLogin {
		if cookie, err := request.Cookie(sessionCookieName); err == nil {
			if id, found := s.sessions.lookup(cookie.Value); found {
				return id, true
			}
		}
	}
	return "", false
}

// isDelegator reports whether an authenticated caller may act for someone else.
func (s *Server) isDelegator(callerID string) bool {
	for _, permitted := range s.options.Delegators {
		if permitted == callerID {
			return true
		}
	}
	return false
}

// resolveUser turns an asserted user id into a Subject with effective roles. The
// resolver is the authority on roles — a caller never asserts its own.
func (s *Server) resolveUser(request *http.Request, userID string) (mantlekeep.Subject, bool) {
	subject, err := s.door.Identity.Resolve(
		request.Context(), mantlekeep.ExternalIdentity{ID: userID})
	if err != nil {
		return mantlekeep.Subject{}, false
	}
	return subject, true
}

// handleDevLogin mints a session for a named user WITHOUT any credential check. It is
// registered only when Options.DevLogin is set, so this cannot be reached in a
// deployment that did not deliberately ask for it.
func (s *Server) handleDevLogin(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.User == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"error": "expected {\"user\":\"…\"}"})
		return
	}
	// Reject an unknown user here rather than at the first govern call, so a typo fails
	// where it was made.
	if _, ok := s.resolveUser(request, body.User); !ok {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"error": "unknown subject " + body.User})
		return
	}

	token := s.sessions.create(body.User)
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(writer, http.StatusOK, map[string]any{"user": body.User})
}

// sessionStore holds dev sessions in memory. Deliberately not durable: sessions from a
// credential-free login must not survive a restart.
type sessionStore struct {
	mutex   sync.RWMutex
	byToken map[string]string
}

func newSessionStore() *sessionStore {
	return &sessionStore{byToken: map[string]string{}}
}

func (store *sessionStore) create(userID string) string {
	token := randomHex(16)
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.byToken[token] = userID
	return token
}

func (store *sessionStore) lookup(token string) (string, bool) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	userID, found := store.byToken[token]
	return userID, found
}

// newIntentID gives each governed request a unique, sortable id for the audit record.
func newIntentID() string {
	return "INT-" + time.Now().UTC().Format("20060102-150405") + "-" + randomHex(4)
}

func randomHex(byteCount int) string {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand failing is not a recoverable application condition.
		panic("doorserver: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(buffer)
}
