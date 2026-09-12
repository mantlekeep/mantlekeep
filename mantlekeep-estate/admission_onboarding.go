package estate

import (
	"context"
	"fmt"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// AdmitApp onboards one app to one environment: the door decides, then the record is written.
//
// The ORDER is the whole thing, and it is the order every other act in this module follows —
// govern, then execute. Writing the record first and submitting afterwards would mean a denied
// onboarding had already onboarded the app, and the chain would carry a denial for a change
// that took effect.
//
// Whether onboarding needs a second person is the POLICY's answer, not this function's: the
// door rules on action "estate.admit" like any other, so a deployment that wants two names on
// an admission to a shared environment says so where it says everything else. Hard-coding it
// here would put one deployment's org chart in a framework.
func AdmitApp(ctx context.Context, door mantlekeep.Submitter, store Admissions,
	gatekeeper mantlekeep.Subject, admission Admission) error {

	return governedAdmissionChange(ctx, door, gatekeeper, admission, admissionActionAdmit,
		func(decided Admission) error { return store.Admit(ctx, decided) })
}

// RevokeApp withdraws an admission, in the same order for the same reason.
//
// Revocation does NOT touch what is already running — nothing here reaches a cluster. It stops
// the NEXT deploy, and a running app is removed by whatever removes apps. Saying so plainly
// matters: a revocation somebody believed was a shutdown is worse than no revocation, because
// they stop looking.
func RevokeApp(ctx context.Context, door mantlekeep.Submitter, store Admissions,
	gatekeeper mantlekeep.Subject, admission Admission) error {

	return governedAdmissionChange(ctx, door, gatekeeper, admission, admissionActionRevoke,
		func(decided Admission) error { return store.Revoke(ctx, decided) })
}

// The two actions a policy rules on, named once so the string a deployment writes in its policy
// and the string submitted here cannot drift.
const (
	admissionActionAdmit  = "estate.admit"
	admissionActionRevoke = "estate.revoke"
)

// governedAdmissionChange holds the order once, so admit and revoke cannot drift apart. The
// classic way one of a pair ends up ungoverned is the second being written months later by
// somebody who read the first and reproduced most of it.
func governedAdmissionChange(ctx context.Context, door mantlekeep.Submitter,
	gatekeeper mantlekeep.Subject, admission Admission, action string,
	write func(Admission) error) error {

	// WHO decided and WHEN are stamped here, from the subject the door is about to rule on and
	// from our clock. Taking either from the caller would let the record name somebody who was
	// not there, or backdate an onboarding to before the ticket that authorised it — and the
	// record is the thing an auditor reads instead of asking us.
	admission.DecidedBy = gatekeeper.ID
	admission.DecidedAt = time.Now().UTC()

	if err := admission.Validate(); err != nil {
		// Refused before the door, because an unattributable decision is not one the door
		// could usefully rule on: there is nobody in it to hold to it afterwards.
		return err
	}
	if _, err := door.Submit(ctx, admissionIntent(admission, action, gatekeeper)); err != nil {
		return fmt.Errorf("%s %s/%s in %q: %w", action, admission.Team, admission.App,
			admission.Env, err)
	}
	return write(admission)
}

// admissionIntent describes an onboarding to the door in the door's own vocabulary.
//
// The environment travels as a PARAM, never as part of the action name. An action per
// environment — "estate.admit.prod" — would make the framework enumerate environments to build
// the string, which is the enum this design refuses; as a param, a policy can say "admitting to
// anything the fleet calls production needs two names" using its own words for production.
func admissionIntent(admission Admission, action string,
	gatekeeper mantlekeep.Subject) mantlekeep.Intent {

	return mantlekeep.Intent{
		ID: fmt.Sprintf("ADMIT-%s-%s-%s-%d", admission.Team, admission.App, admission.Env,
			admission.DecidedAt.UnixNano()),
		// The subject WHOLE, not rebuilt from the stored id: the door resolves roles from the
		// directory and a reconstructed subject would arrive with none, so a policy about who
		// may onboard to a production environment would have nothing to rule on.
		Subject: gatekeeper,
		Action:  action,
		// The team's namespace, the same resource shape estate.apply uses, so one policy can
		// speak about a team's onboarding and a team's deploys without two vocabularies.
		Resource: "team/" + admission.Team,
		Spec: mantlekeep.IntentSpec{
			Goal: fmt.Sprintf("%s %s/%s in environment %q", action, admission.Team,
				admission.App, admission.Env),
		},
		Params: map[string]any{
			"app": admission.App,
			// Opaque, and passed through exactly as the deployment named it.
			"env": admission.Env,
			// The ticket or record id. On the chain because this record IS the onboarding
			// evidence — the alternative was a metadata service beside the chain, and a second
			// source of truth that can disagree with the chain is worse than none.
			"reference": admission.Reference,
			"reason":    admission.Reason,
			"scope":     admission.Team,
		},
		SubmittedAt: admission.DecidedAt,
		TTL:         5 * time.Minute,
	}
}
