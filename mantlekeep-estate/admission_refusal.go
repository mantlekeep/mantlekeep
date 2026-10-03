package estate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// NotAdmitted is a refusal somebody can act on.
//
// It carries the gatekeeper's own words and the reference the chain cites, because those two
// facts are the difference between "your deploy was refused, here is who onboarded this app and
// what ticket to quote" and a red cross in a pipeline. A refusal with neither teaches people to
// route around the control, which is the only way a control like this actually fails.
type NotAdmitted struct {
	Team string
	App  string
	Env  string
	// Reason is the gatekeeper's sentence, or ours when there is no record to quote.
	Reason string
	// Reference is the ticket or record id to quote. EMPTY when no record exists at all —
	// there is no decision to cite yet, and admitting the app is the act that creates one.
	Reference string
	// Cause is the sentinel this refusal is, for errors.Is: ErrNotAdmitted, ErrAdmissionNotFound
	// or ErrAdmissionStoreUnreachable.
	Cause error
}

// Error reads as an instruction, because the person reading it is mid-deploy and needs to know
// what to change rather than what went wrong.
func (n *NotAdmitted) Error() string {
	where := fmt.Sprintf("%s/%s is not admitted to environment %q", n.Team, n.App, n.Env)
	if n.Reference == "" {
		return fmt.Sprintf("estate: %s: %s — admitting an app to an environment is itself a "+
			"governed act, so ask the platform to admit it and the record id it creates becomes "+
			"the reference for this app", where, n.Reason)
	}
	return fmt.Sprintf("estate: %s: %s (reference %s) — quote that reference to whoever owns "+
		"this environment", where, n.Reason, n.Reference)
}

// Unwrap lets a caller branch on the KIND of refusal without matching on a message.
func (n *NotAdmitted) Unwrap() error { return n.Cause }

// RequireAdmission answers THE question for one app in one environment, and FAILS CLOSED.
//
// Every path that cannot confirm an admission returns a refusal: no store configured, no
// environment on the change, the store erroring, no record, a revoked record, a lapsed one.
// None of them assume admitted. That is what makes the store a hard dependency (see
// [ErrAdmissionStoreUnreachable]) and it is the trade being made deliberately: a control that
// opens when its data is unavailable is not a control, it is a control-shaped outage that fails
// in the permissive direction.
//
// This is NOT the gate. The gate — [Floor.GateFor] — asks whether a person must sign this
// change. This asks whether the app may be in this environment at all, and the two are
// answered separately on purpose: an admitted app still needs its signature, and a signature
// can never admit an app. Neither is implemented in terms of the other, so a change to one
// cannot weaken the other by accident.
func RequireAdmission(ctx context.Context, store Admissions, team, app, env string) error {
	if store == nil {
		return &NotAdmitted{Team: team, App: app, Env: env, Cause: ErrAdmissionStoreUnreachable,
			Reason: "no admission store is configured, so no app can be confirmed as onboarded " +
				"anywhere — this refuses rather than waving everything through"}
	}
	if env == "" {
		// An unnamed environment cannot be looked up, and treating it as "any" would admit an
		// app to every environment at once — the precise failure this control exists to stop.
		return &NotAdmitted{Team: team, App: app, Env: env, Cause: ErrNotAdmitted,
			Reason: "this change names no environment, so there is nothing to check it against"}
	}
	record, err := store.Get(ctx, team, app, env)
	switch {
	case errors.Is(err, ErrAdmissionNotFound):
		return &NotAdmitted{Team: team, App: app, Env: env, Cause: ErrAdmissionNotFound,
			Reason: "this app has never been onboarded to this environment"}
	case err != nil:
		return &NotAdmitted{Team: team, App: app, Env: env, Cause: ErrAdmissionStoreUnreachable,
			Reason: fmt.Sprintf("the admission store could not answer (%v), and an "+
				"unconfirmed admission is refused rather than assumed", err)}
	}
	now := time.Now().UTC()
	if record.Admitted(now) {
		return nil
	}
	// The gatekeeper's own words, never a paraphrase: the record says why this app was taken
	// out of this environment, and re-wording it here would hand the reader our sentence
	// instead of the one somebody is accountable for. A LAPSED admission is the exception —
	// nobody wrote a sentence about it lapsing, so we say that plainly and keep their reason
	// attached, because the reason it was granted is what somebody will want to renew.
	reason := record.Reason
	if record.lapsed(now) {
		reason = fmt.Sprintf("its admission lapsed at %s and was not renewed; it was granted "+
			"because: %s", record.ExpiresAt.Format(time.RFC3339), record.Reason)
	}
	return &NotAdmitted{Team: team, App: app, Env: env, Cause: ErrNotAdmitted,
		Reason: reason, Reference: record.Reference}
}
