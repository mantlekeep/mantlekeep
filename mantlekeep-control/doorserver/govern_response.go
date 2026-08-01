package doorserver

import (
	"net/http"
	"strings"

	mantlekeep "mantlekeep.dev/control"
)

// The typed denial taxonomy. A product maps these codes to HTTP status and behaviour
// deterministically, instead of parsing free-text reasons. The codes are a WIRE concern —
// the engine produces a Decision with an action and a human reason; the code is derived
// here, at the boundary, so the core never names a product-facing taxonomy.
const (
	codeFloor            = "DENY_FLOOR"
	codeSeparationDuties = "DENY_SEPARATION_OF_DUTIES"
	codeIdentity         = "DENY_IDENTITY"
	codeActionNotAllowed = "DENY_ACTION_NOT_ALLOWED"
	codeValidation       = "DENY_VALIDATION"
	codePolicyError      = "DENY_POLICY_ERROR"
)

// wireReason is one typed reason on the wire: a stable code plus the human message.
type wireReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// denialCode classifies a deny reason into a stable code. Order matters: the more specific
// identity and separation-of-duties cases are matched before the general ones.
func denialCode(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.Contains(r, "separation of duties"),
		strings.Contains(r, "may not be the requester"),
		strings.Contains(r, "approve their own"):
		return codeSeparationDuties
	case strings.Contains(r, "unknown subject"),
		strings.Contains(r, "unauthenticated"),
		strings.Contains(r, "no caller identity"),
		strings.Contains(r, "not permitted to act"):
		return codeIdentity
	case strings.Contains(r, "goal is required"),
		strings.Contains(r, "malformed"),
		strings.Contains(r, "is required"):
		return codeValidation
	case strings.Contains(r, "no role permits"),
		strings.Contains(r, "not allowed"),
		strings.Contains(r, "not permitted"):
		return codeActionNotAllowed
	case strings.Contains(r, "exceeds"),
		strings.Contains(r, "cap"),
		strings.Contains(r, "unapproved"),
		strings.Contains(r, "not an approved"),
		strings.Contains(r, "requires an elevated"),
		strings.Contains(r, "floor"),
		strings.Contains(r, "pinned"),
		strings.Contains(r, "sha256"):
		return codeFloor
	default:
		return codePolicyError
	}
}

// statusForDeny maps a typed deny code to an HTTP status. A refusal is an answer, never a
// server fault, so it is a 4xx: identity is 401, a malformed request 400, everything else
// 403 (the door answered, and the answer was no).
func statusForDeny(code string) int {
	switch code {
	case codeIdentity:
		return http.StatusUnauthorized
	case codeValidation:
		return http.StatusBadRequest
	default:
		return http.StatusForbidden
	}
}

// writeAllow emits the canonical allow response: the token, the policy that authorised it,
// when it expires, and an empty reasons list (a stable shape a client can always read).
func writeAllow(writer http.ResponseWriter, token mantlekeep.ExecutionToken) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"outcome":   "allow",
		"token":     token.Value,
		"intentId":  token.IntentID,
		"policyId":  token.PolicyID,
		"expiresAt": token.ExpiresAt.Format(rfc3339),
		"reasons":   []wireReason{},
	})
}

// writeDecision emits a deny or require_approval from a Decision the engine produced.
// intentId is passed separately because a non-allow yields no token.
func writeDecision(writer http.ResponseWriter, decision mantlekeep.Decision, intentID string) {
	body := map[string]any{
		"outcome":  string(decision.Action),
		"intentId": intentID,
		"policyId": decision.PolicyID,
		"reasons":  []wireReason{{Code: denialCode(decision.Reason), Message: decision.Reason}},
	}

	status := statusForDeny(denialCode(decision.Reason))
	if decision.Action == mantlekeep.ActionRequireApproval {
		// require_approval is not a refusal — it is "not yet". The approvers a second
		// party would need are named so the caller can route it; the outcome is 200 and
		// lives in the body.
		status = http.StatusOK
		approvers := make([]string, 0, len(decision.RequiredApprovers))
		for _, role := range decision.RequiredApprovers {
			approvers = append(approvers, string(role))
		}
		body["requiredApprovers"] = approvers
	}

	writeJSON(writer, status, body)
}

// rfc3339 is the timestamp format for the wire — a stable, parseable instant.
const rfc3339 = "2006-01-02T15:04:05Z07:00"
