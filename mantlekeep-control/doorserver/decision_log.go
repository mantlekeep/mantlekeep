package doorserver

import (
	"log/slog"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// This file is ONE concern: structured, console observability of door decisions.
//
// It sits ALONGSIDE the audit chain (audit.go), never in place of it. The chain is the
// tamper-evident evidence of record; this is the operator's live view — one structured line
// per finalized decision, so a deny is visible in `kubectl logs` the moment it happens without
// walking the ledger. It is emitted from the ONE place that sees every decision with its caller:
// the govern handler, after the door has answered.
//
// SECURITY — what is logged is DELIBERATELY only decision METADATA: outcome, action, subject,
// policy, and on a non-allow the generic category + human reason. It NEVER logs intent params
// (which carry request payload — amounts, regions, arbitrary values), the execution token, or
// any approval token. A reviewer can confirm this by inspection: decisionLog has no field for a
// param map or a token, so there is nothing secret to leak here. Every value is passed as a typed
// slog attribute (key/value), never concatenated into a message, so a request-derived string such
// as `action` cannot forge a second log line.
//
// CodeQL's go/clear-text-logging flags the `subject` and `via` attributes here, because it
// treats any value read from an HTTP request header as sensitive — headers commonly carry
// Authorization and Cookie. What reaches these fields is the caller IDENTITY resolved from
// the trusted identity header (see identity.go), which is the one thing a decision log
// exists to record; the execution token never comes near this file. The credential case
// CodeQL is generalising from is prevented at construction instead: New refuses an identity
// header that names a credential (checkIdentityHeader), so the value logged here cannot be
// a bearer token even under a misconfiguration.

// decisionLog is the SAFE-TO-LOG projection of a finalized door decision. The field set is the
// whole security argument: there is no place to put a secret. category and reason are populated
// only on a non-allow.
type decisionLog struct {
	outcome  mantlekeep.DecisionAction
	action   string
	subject  string
	via      string                    // the delegating service, when one carried the claim
	policyID string                    // which policy produced the verdict
	category mantlekeep.DenialCategory // generic denial class; empty on an allow
	reason   string                    // human justification; empty on an allow
}

// emit writes exactly one structured line: allow at INFO, deny/require_approval at WARN so a
// refusal stands out. category + reason are attached only on a non-allow.
func (d decisionLog) emit() {
	attrs := []any{
		slog.String("outcome", string(d.outcome)),
		slog.String("action", d.action),
		slog.String("subject", d.subject),
		slog.String("policyId", d.policyID),
	}
	if d.via != "" {
		attrs = append(attrs, slog.String("via", d.via))
	}
	if d.outcome == mantlekeep.ActionAllow {
		slog.Info("door decision", attrs...)
		return
	}
	attrs = append(attrs,
		slog.String("category", string(d.category)),
		slog.String("reason", d.reason),
	)
	slog.Warn("door decision", attrs...)
}
