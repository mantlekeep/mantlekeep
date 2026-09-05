package web

import (
	"net/http"
	"strings"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// statusFor maps an apply outcome onto an HTTP status.
//
// This is TRANSLATION, not policy. Nothing here decides anything — the door already did, per
// change — and the rule is only about not telling a caller that something happened when it did
// not.
//
// A MIXED outcome is 200. That is the designed case, not a compromise: a manifest holds a
// playground topic and a production one, the playground is provisioned immediately and the
// production one waits for a person, and the response body says exactly which is which.
// Failing the whole request because one change was gated is the over-gating that makes a golden
// path slower than the bypass, and a bypassed guardrail governs nothing.
//
// Refused everything and applied nothing is NOT 200. The caller asked for work, none of it
// happened, and a success status would make `curl -f` and every client written against it treat
// a wholly refused change as done.
func statusFor(outcome estate.ApplyOutcome) int {
	if len(outcome.Refused) == 0 {
		return http.StatusOK
	}
	if len(outcome.Applied) > 0 || len(outcome.Failed) > 0 {
		return http.StatusOK
	}

	sawApprovalNeeded := false
	for _, result := range outcome.Refused {
		switch decisionIn(result.Refused) {
		case string(mantlekeep.ActionRequireApproval):
			sawApprovalNeeded = true
		case string(mantlekeep.ActionDeny):
			// A flat refusal; keep looking in case something is merely pending.
		default:
			// Not a decision at all — the door did not answer, or answered off-contract. That
			// is a fault upstream of the caller, and calling it Forbidden would tell a team
			// their change was refused when nobody ever ruled on it.
			return http.StatusBadGateway
		}
	}
	if sawApprovalNeeded {
		// Pending a person, not forbidden. Conflict, because the request is well-formed and
		// permitted and simply cannot proceed in the estate's current state.
		return http.StatusConflict
	}
	return http.StatusForbidden
}

// decisionIn reads the door's decision word off a refusal.
//
// The door renders every refusal as "<decision>: <reason>" — in-process and over HTTP alike —
// so the word is recoverable without a second channel. It is read rather than re-derived: the
// alternative is this layer forming its own opinion of what the door meant, which is how a
// caller ends up with two different sentences for one decision.
func decisionIn(refusal string) string {
	decision, _, found := strings.Cut(refusal, ": ")
	if !found {
		return ""
	}
	switch decision {
	case string(mantlekeep.ActionDeny), string(mantlekeep.ActionRequireApproval):
		return decision
	default:
		return ""
	}
}
