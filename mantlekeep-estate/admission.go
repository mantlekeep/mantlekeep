package estate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AdmissionState is whether an app may exist in an environment RIGHT NOW.
//
// Three values and no more. There is no "requested", no "in review", no "pending onboarding":
// those are stages of a workflow, and a workflow is a second question wearing the clothes of
// this one. An app either has a standing decision that it belongs here or it does not, and
// anything mid-flight is not a decision yet.
type AdmissionState string

const (
	// AdmissionAdmitted is a standing decision that this app belongs in this environment.
	AdmissionAdmitted AdmissionState = "admitted"
	// AdmissionRevoked is a standing decision that it no longer does. Distinct from never
	// having been admitted: "we took this out" and "this was never onboarded" send somebody
	// looking in different places, and only one of them has a decision to read.
	AdmissionRevoked AdmissionState = "revoked"
	// AdmissionExpired is a time-boxed admission that ran out. Derived on READ and never
	// written — see [MemoryAdmissions.Get] for why a rule a sweeper has to run is not a rule.
	AdmissionExpired AdmissionState = "expired"
)

// Admission is the standing answer to ONE question: may this app exist in this environment.
//
// # Why it is not a caller check
//
// "May this CALLER touch this app" has no useful answer in a deployment where one service
// account deploys every project, owned by one person, triggered by both CI and a push webhook.
// The caller is always the same caller, so a rule about the caller separates nothing. What can
// still be separated is the APP and the PLACE: an app that was never onboarded to an environment
// cannot be deployed there by anyone — not by a rogue commit, and not by a typo in a chart name
// that happens to resolve.
//
// # It is also the onboarding record
//
// Admitting an app is itself a governed action, so "who onboarded this, when, and against which
// ticket" is a chain record rather than a spreadsheet. That is deliberate instead of a metadata
// service beside the chain: a second source of truth that can disagree with the chain is worse
// than none, because the disagreement is discovered by an auditor rather than by us.
type Admission struct {
	Team string `json:"team"`
	// App is the app identity as a governed change carries it. A resolved app change is named
	// by the deployment it becomes, so this must be that name — matching on anything else would
	// mean re-deriving the resolver's prefixing rule here, and a convention re-derived in two
	// places is a convention that drifts in one of them.
	App string `json:"app"`
	// Env is an OPAQUE STRING and this module never interprets it.
	//
	// No enum, no constant, no ordering. A deployment whose promotion order is SIT → DEV | UAT
	// → PROD must work exactly as well as one that goes DEV → SIT → UAT → PROD, and a
	// framework that knew any of those names would be asserting a lifecycle it was never told.
	// The same discipline as [Floor.EnvTiers], which maps a configured env name to a
	// consequence class and knows nothing else about it.
	Env string `json:"env"`

	State AdmissionState `json:"state"`
	// Reason is the gatekeeper's OWN WORDS for the current state — why this app was onboarded
	// here, or why it was taken out. Carried because a refusal that cannot say why is a dead
	// end wearing the shape of a process.
	Reason string `json:"reason"`
	// Reference is the ticket or record id this decision was made against, and it is what the
	// chain cites. It is the whole answer to an auditor asking "why is this here": without it
	// the refusal names no next step and the admission names no authority.
	Reference string `json:"reference"`
	// DecidedBy is the person who onboarded this app here, or withdrew it. NOT the caller of
	// the deploy — the deploy caller is the account this control exists because it cannot
	// distinguish.
	DecidedBy string    `json:"decidedBy"`
	DecidedAt time.Time `json:"decidedAt"`
	// ExpiresAt bounds the admission, zero meaning it does not expire.
	//
	// Optional because most onboarding is permanent, and present because some is not: an
	// experiment admitted to a shared environment for a fortnight should stop being admitted
	// when the fortnight ends, without anybody remembering to come back. An admission that can
	// only be ended by hand is one that outlives the app it was granted for.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// Admitted reports whether this record permits the app to exist, expiry included — because a
// lapsed admission is not an admission however its state field reads if nothing has swept it.
func (a Admission) Admitted(now time.Time) bool {
	if a.State != AdmissionAdmitted {
		return false
	}
	return a.ExpiresAt.IsZero() || now.Before(a.ExpiresAt)
}

// lapsed reports a time-boxed admission whose time has passed, whether or not a store has got
// round to writing [AdmissionExpired] over it. Computed rather than read, so a store that
// forgets to mark expiry still cannot make a lapsed admission behave like a live one.
func (a Admission) lapsed(now time.Time) bool {
	if a.State == AdmissionExpired {
		return true
	}
	return a.State == AdmissionAdmitted && !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt)
}

// Validate refuses a decision that could not be explained afterwards.
//
// A record with no reference and no decider still answers "may this app be here" perfectly
// well, and answers "who said so" not at all — so the refusal it later produces names nobody to
// ask. That makes an unattributable admission worse than a missing one: it is a control that
// works until the day somebody needs to know why.
func (a Admission) Validate() error {
	if a.Team == "" || a.App == "" || a.Env == "" {
		return fmt.Errorf("%w: an admission needs a team, an app and an environment, got %q/%q in %q",
			ErrAdmissionUnattributable, a.Team, a.App, a.Env)
	}
	if a.Reference == "" {
		return fmt.Errorf("%w: %s/%s in %q has no reference — the reference is what the chain "+
			"cites, so without one no auditor can be told why this app is here",
			ErrAdmissionUnattributable, a.Team, a.App, a.Env)
	}
	if a.DecidedBy == "" {
		return fmt.Errorf("%w: %s/%s in %q names nobody who decided it — a refusal derived from "+
			"this record could not say who to ask", ErrAdmissionUnattributable, a.Team, a.App, a.Env)
	}
	if a.Reason == "" {
		return fmt.Errorf("%w: %s/%s in %q carries no reason — a refusal must be able to quote "+
			"the gatekeeper rather than paraphrase them",
			ErrAdmissionUnattributable, a.Team, a.App, a.Env)
	}
	return nil
}

// Errors a caller must be able to tell apart, because each one is a different thing to do next.
var (
	// ErrNotAdmitted — this app may not exist in this environment. The answer to THE question,
	// and always wrapped by a [NotAdmitted] carrying the words and the reference.
	ErrNotAdmitted = errors.New("estate: this app is not admitted to this environment")
	// ErrAdmissionNotFound — no record either way. Kept distinct from a revoked one: nothing to
	// read is an onboarding that never happened, and a revocation is a decision somebody made.
	ErrAdmissionNotFound = errors.New("estate: no admission record for this app in this environment")
	// ErrAlreadyAdmitted — admitting twice. Refused rather than merged, because the second
	// record would quietly replace the reference an auditor is meant to follow, and the first
	// decision would vanish with no trace that it was ever the one in force.
	ErrAlreadyAdmitted = errors.New(
		"estate: this app is already admitted to this environment — revoke it first if the " +
			"decision has changed, so both acts stay on the chain")
	// ErrNothingToRevoke — revoking what is not admitted. A no-op that reported success would
	// let somebody believe they had removed an app from an environment it was never in, while
	// the environment it IS in keeps running it.
	ErrNothingToRevoke = errors.New(
		"estate: this app is not admitted to this environment, so there is nothing to revoke")
	// ErrAdmissionUnattributable — a decision that could not be explained later. See
	// [Admission.Validate].
	ErrAdmissionUnattributable = errors.New("estate: this admission could not be attributed")
	// ErrAdmissionStoreUnreachable — the store could not answer, so the answer is NO.
	//
	// This makes the store a HARD DEPENDENCY, said plainly rather than discovered: with it
	// unavailable nothing deploys. That is the trade being made on purpose, because the other
	// failure mode is that a store outage admits everything to everywhere for as long as it
	// lasts, and nothing in the record afterwards distinguishes those deploys from approved
	// ones.
	ErrAdmissionStoreUnreachable = errors.New(
		"estate: the admission store could not be reached, so admission cannot be confirmed")
)

// Admissions remembers which apps may exist in which environments.
//
// A port rather than a table, for the same reason [Approvals] is one: an admission outlives
// every process that reads it — that is what makes it a standing decision rather than a check —
// so where it lives is a deployment's choice. The core knows only this interface; which store
// backs it is the adapter's knowledge.
//
// FOUR methods, and the shape is the point. There is no Request, no Transition, no Stage: an
// in-house system that governed this grew stages until people worked around all of them, and
// the value here is entirely in the narrowness.
type Admissions interface {
	// Get returns the standing record for one app in one environment, or ErrAdmissionNotFound.
	// An implementation must report a lapsed admission as expired, not admitted.
	Get(ctx context.Context, team, app, env string) (Admission, error)
	// Admit records that an app may exist in an environment. It must refuse, with
	// ErrAlreadyAdmitted, to overwrite a record that is currently admitted — otherwise two
	// onboardings race and the reference an auditor follows is whichever one wrote last.
	Admit(ctx context.Context, admission Admission) error
	// Revoke withdraws an admission, refusing with ErrNothingToRevoke anything not currently
	// admitted. The record is REPLACED rather than deleted: a deleted admission is
	// indistinguishable from an app that was never onboarded, and the revocation is exactly the
	// decision somebody will need to read.
	Revoke(ctx context.Context, admission Admission) error
	// AdmittedIn lists what may exist in one environment or — with an empty env — everywhere,
	// so the platform can read its own onboarding rather than infer it from what happens to be
	// running.
	AdmittedIn(ctx context.Context, env string) ([]Admission, error)
}
