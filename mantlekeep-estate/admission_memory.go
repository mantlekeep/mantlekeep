package estate

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryAdmissions keeps admission records in memory.
//
// HONEST about what it is: a restart forgets every onboarding, and because
// [RequireAdmission] fails closed, a restart therefore refuses every app until they are
// onboarded again. That is unacceptable in a deployment and correct for a demo — the failure
// direction is at least the safe one, which is not something a caching layer in front of a real
// store would give for free.
//
// See TestTwoAdmissionStoresCannotCoordinate for the limit that matters more than persistence.
type MemoryAdmissions struct {
	mu sync.RWMutex
	// by is keyed by team, app and environment together. All three, because the same app name
	// exists in several teams and the same app is legitimately admitted to one environment and
	// not another — that difference IS the control, so it cannot be collapsed into the key.
	by map[string]Admission
	// now is injectable so a test can drive expiry without sleeping.
	now func() time.Time
}

// NewMemoryAdmissions returns an empty in-memory store.
//
// Empty means nothing is admitted anywhere, so a manager wired to a fresh one refuses every
// app. That is the right default: an estate where onboarding is implied by deploying is an
// estate with no onboarding.
func NewMemoryAdmissions() *MemoryAdmissions {
	return &MemoryAdmissions{by: map[string]Admission{}, now: time.Now}
}

var _ Admissions = (*MemoryAdmissions)(nil)

// admissionKey names one app in one environment.
//
// The environment is appended verbatim — never normalised, never lower-cased. A deployment that
// runs both "canary" and "Canary" has two environments, only the deployment knows whether they
// are the same place, and folding them here would admit an app somewhere nobody ruled on while
// every record still looked right.
func admissionKey(team, app, env string) string {
	return team + "/" + app + "@" + env
}

// Get returns the standing record, marking a lapsed admission expired on read.
//
// Expiry is evaluated on READ rather than by a sweeper, for the reason
// [MemoryApprovals.Get] gives: a store with no background loop must still be unable to hand
// back a lapsed admission as live. A sweeper may exist as an optimisation; it must never be
// what makes the rule true.
func (m *MemoryAdmissions) Get(_ context.Context, team, app, env string) (Admission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.by[admissionKey(team, app, env)]
	if !ok {
		return Admission{}, ErrAdmissionNotFound
	}
	if record.lapsed(m.now()) {
		record.State = AdmissionExpired
	}
	return record, nil
}

// Admit records that an app may exist in an environment, refusing to overwrite a live admission.
//
// The read and the write are under ONE lock hold on purpose. Two onboardings for the same app
// and environment would otherwise both read "nothing here", both write, and the reference an
// auditor follows would be whichever landed second — with no trace that another ticket ever
// authorised the same thing.
//
// A revoked or lapsed record IS replaced, and that is not the same relaxation. Re-onboarding an
// app that was taken out is a normal act with its own reference, and the record it replaces is
// on the chain already: the store holds what is true now, the chain holds what happened.
func (m *MemoryAdmissions) Admit(_ context.Context, admission Admission) error {
	if err := admission.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := admissionKey(admission.Team, admission.App, admission.Env)
	if current, ok := m.by[key]; ok && current.Admitted(m.now()) {
		return ErrAlreadyAdmitted
	}
	admission.State = AdmissionAdmitted
	m.by[key] = admission
	return nil
}

// Revoke withdraws an admission, refusing anything that is not currently admitted.
//
// Same single lock hold, for the mirror-image reason: two revocations would both succeed and
// the second would overwrite the first's reason, so the sentence a deployer later reads would
// not be the one belonging to the decision that actually removed their app.
//
// The record is replaced, never deleted. A deleted admission reads exactly like an app nobody
// ever onboarded, and "we took this out, here is why" is the single most useful thing a refusal
// can say.
func (m *MemoryAdmissions) Revoke(_ context.Context, admission Admission) error {
	if err := admission.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := admissionKey(admission.Team, admission.App, admission.Env)
	current, ok := m.by[key]
	if !ok {
		return ErrAdmissionNotFound
	}
	if !current.Admitted(m.now()) {
		return ErrNothingToRevoke
	}
	admission.State = AdmissionRevoked
	m.by[key] = admission
	return nil
}

// AdmittedIn lists what may exist in one environment, or everywhere with an empty env.
//
// Only live admissions appear. A revoked one in this list would read as an app the platform
// still expects to be running, which is how a decommissioned app gets redeployed by whoever
// tidies up the inventory.
func (m *MemoryAdmissions) AdmittedIn(_ context.Context, env string) ([]Admission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.now()
	live := make([]Admission, 0, len(m.by))
	for _, record := range m.by {
		if !record.Admitted(now) {
			continue
		}
		if env != "" && record.Env != env {
			continue
		}
		live = append(live, record)
	}
	// Ordered by the identity a person reads, not by decision time: this answers "what is
	// onboarded here", which is an inventory, and an inventory that reshuffles between two
	// reads cannot be diffed by eye or by a script.
	sort.Slice(live, func(first, second int) bool {
		return admissionKey(live[first].Team, live[first].App, live[first].Env) <
			admissionKey(live[second].Team, live[second].App, live[second].Env)
	})
	return live, nil
}
