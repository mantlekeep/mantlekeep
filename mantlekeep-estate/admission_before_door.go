package estate

import "context"

// AdmissionCheck stops a change whose app is not admitted to the environment it would land in,
// before the change reaches the door.
//
// # Why it hangs off the transform seam
//
// This is the ONLY place today where a deployment can get in front of every change the manager
// governs without editing the manager. [ChangeTransformer] runs on the Apply path and on the
// Reconcile path, before [Manager.applyOne] submits anything, and a caller cannot skip it — the
// ordering is held by where it is called from rather than by a flag. So admission checked here
// genuinely cannot be routed around, which is the property that matters most.
//
// # Why this is a demonstration and not the finished wiring
//
// Two mismatches, both honest:
//
//   - a transform error is reported as [Result.Failed], not [Result.Refused], and the manager
//     says why: Refused carries the DOOR's own words, and the door was never asked. So a
//     correct admission refusal arrives labelled as a failure, which invites a retry.
//   - [ChangeTransformer] asks implementations to be deterministic and self-contained. This one
//     reads a store, so the same input can legitimately give two answers on two days.
//
// Both disappear once the check sits in applyOne ahead of the door submission, where a refusal
// is a refusal and reading a store is normal. NOTES.md carries that diff.
type AdmissionCheck struct {
	admissions Admissions
}

// CheckAdmissionIn builds the check over one store.
//
// A nil store is NOT rejected here, because rejecting it would make the mistake at wiring time
// and the mistake worth catching is at decision time: [RequireAdmission] refuses every app when
// it has no store, so a deployment that mis-wires this finds out by nothing deploying rather
// than by everything deploying ungoverned.
func CheckAdmissionIn(store Admissions) *AdmissionCheck {
	return &AdmissionCheck{admissions: store}
}

var _ ChangeTransformer = (*AdmissionCheck)(nil)

// Transform passes a change through untouched, or refuses it.
//
// It rewrites NOTHING. Admission answers one question and the answer is yes or no; a check that
// also edited the change would be two things in one seam, and the edit would be the part nobody
// reviewed.
func (c *AdmissionCheck) Transform(ctx context.Context, team string,
	change DesiredItem) (DesiredItem, error) {

	env, governed := admissionEnvironmentOf(change)
	if !governed {
		return change, nil
	}
	if err := RequireAdmission(ctx, c.admissions, team, change.Name, env); err != nil {
		return DesiredItem{}, err
	}
	return change, nil
}

// admissionEnvironmentOf reports which environment a change lands in, and whether admission has
// anything to say about it at all.
//
// Only an APP change is ruled on, because an app is the only thing in a resolved change that
// carries an environment: placement records the environment of the cluster actually chosen, and
// that — not the one declared — is where the app will really be. Every other asset passes
// through, which is a REAL HOLE and not a decision: a Kafka topic or a database has no
// environment in this model, so this cannot rule on one without inventing the field. NOTES.md
// says what the framework would have to add.
//
// An app change with no placement is refused rather than skipped: an app is known to have an
// environment, so a missing one is a change we cannot rule on, and skipping it would turn the
// one asset this control covers into the one asset that gets waved through.
func admissionEnvironmentOf(change DesiredItem) (env string, governed bool) {
	if change.Asset != "app" {
		return "", false
	}
	if change.Placement == nil {
		return "", true
	}
	return change.Placement.Env, true
}
