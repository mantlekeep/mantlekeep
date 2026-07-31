package registry

import (
	"context"
	"fmt"
)

// PromotionPolicy is THIS env's restrictiveness — config, never hardcoded. dev/sit
// run loose (RequiresApproval=false → a proposed version publishes immediately);
// uat/prod run sealed (RequiresApproval=true + RequireSoD → a required approver, who
// must differ from the proposer, signs before a version publishes). Each env
// instance is constructed with its own policy, so the same primitive is loose in
// dev and sealed in prod without any code change.
type PromotionPolicy struct {
	RequiresApproval         bool // a proposed version waits in review for an approver
	RequireSoD               bool // that approver must not be the proposer
	RequireTestBeforePromote bool // a draft must record a passing test-run before it can be proposed
}

// Ready-made policies (defaults; a team may supply its own). dev iterates freely;
// sealed envs demand a passing test, a required approver, and separation-of-duties.
var (
	LooseDev   = PromotionPolicy{RequiresApproval: false, RequireSoD: false, RequireTestBeforePromote: false}
	SealedProd = PromotionPolicy{RequiresApproval: true, RequireSoD: true, RequireTestBeforePromote: true}
)

// LinkChange attaches an external change-request id (e.g. a ServiceNow/Jira CR) to a
// version. Some orgs route PROD promotion through a CR by agreement with IT — slower,
// but higher-trust — instead of, or alongside, an internal approver. MantleKeep governs
// either way and records the CR id on the chain, so the CR and the door decision are
// one auditable trail. Which path applies is config, never hardcoded.
func (r *Registry) LinkChange(ctx context.Context, name, version, changeRef string) (Version, error) {
	return r.transition(ctx, name, version, func(v *Version) error {
		v.ChangeRef = changeRef
		return nil
	})
}

// RecordTest attaches the outcome of a local/dev test-run to a DRAFT version — the
// "test before promote" evidence you produce while implementing/hotfixing a tool or
// changing a job. ref points at the run (kernel I/O for a tool, a dev job
// run for a flow). A failed test is recorded too, so the draft's history shows what
// was tried. Under a policy that RequireTestBeforePromote, ProposePromote stays
// blocked until a passing test is on record — you cannot promote an untested draft.
func (r *Registry) RecordTest(ctx context.Context, name, version string, passed bool, ref string) (Version, error) {
	return r.transition(ctx, name, version, func(v *Version) error {
		if v.Status != StatusDraft {
			return fmt.Errorf("registry: %s@%s is %q, tests attach to a draft before promote", name, version, v.Status)
		}
		v.TestPassed = passed
		v.TestedAt = r.now()
		v.TestRef = ref
		return nil
	})
}

// ProposePromote asks to publish a DRAFT version into this env. Under a loose policy
// it publishes at once; under a sealed policy it enters REVIEW to await an approver.
func (r *Registry) ProposePromote(ctx context.Context, name, version, proposedBy string) (Version, error) {
	return r.transition(ctx, name, version, func(v *Version) error {
		if v.Status != StatusDraft {
			return fmt.Errorf("registry: %s@%s is %q, only a draft can be proposed", name, version, v.Status)
		}
		if r.policy.RequireTestBeforePromote && !v.TestPassed {
			return fmt.Errorf("registry: %s@%s must pass a test before promote (test-before-promote gate)", name, version)
		}
		v.ProposedBy = proposedBy
		if r.policy.RequiresApproval {
			v.Status = StatusInReview
			return nil
		}
		v.Status = StatusPublished
		v.PublishedAt = r.now()
		return nil
	})
}

// Approve publishes a version awaiting review. Separation-of-duties: when the policy
// requires it, the approver must not be the proposer.
func (r *Registry) Approve(ctx context.Context, name, version, approver string) (Version, error) {
	return r.transition(ctx, name, version, func(v *Version) error {
		if v.Status != StatusInReview {
			return fmt.Errorf("registry: %s@%s is %q, only a version in review can be approved", name, version, v.Status)
		}
		if r.policy.RequireSoD && approver == v.ProposedBy {
			return fmt.Errorf("registry: separation-of-duties — approver %q must differ from proposer", approver)
		}
		v.Status = StatusPublished
		v.ApprovedBy = approver
		v.PublishedAt = r.now()
		return nil
	})
}

// Reject sends a version in review back to draft.
func (r *Registry) Reject(ctx context.Context, name, version, _ string) (Version, error) {
	return r.transition(ctx, name, version, func(v *Version) error {
		if v.Status != StatusInReview {
			return fmt.Errorf("registry: %s@%s is %q, only a version in review can be rejected", name, version, v.Status)
		}
		v.Status = StatusDraft
		v.ProposedBy = ""
		return nil
	})
}

// Deprecate marks a published version deprecated: it still resolves so old flows
// keep running, but it warns and becomes a candidate for demise.
func (r *Registry) Deprecate(ctx context.Context, name, version string) (Version, error) {
	return r.transition(ctx, name, version, func(v *Version) error {
		if v.Status != StatusPublished {
			return fmt.Errorf("registry: %s@%s is %q, only a published version can be deprecated", name, version, v.Status)
		}
		v.Status = StatusDeprecated
		return nil
	})
}

// Demise retires a version so it no longer resolves. It is BLOCKED while flows
// still pin it (the blast radius) unless force is set — so you see who breaks first.
func (r *Registry) Demise(ctx context.Context, name, version string, force bool) (Version, error) {
	if !force {
		dep, err := r.Dependents(ctx, name, version)
		if err != nil {
			return Version{}, err
		}
		if len(dep) > 0 {
			return Version{}, fmt.Errorf("registry: %s@%s still used by %d flow(s) %v — migrate them or force", name, version, len(dep), dep)
		}
	}
	return r.transition(ctx, name, version, func(v *Version) error {
		if v.Status != StatusDeprecated && v.Status != StatusPublished {
			return fmt.Errorf("registry: %s@%s is %q, cannot demise", name, version, v.Status)
		}
		v.Status = StatusDemised
		v.Default = false
		return nil
	})
}

// SetDefault marks one published version as this env's default — the fallback target
// resolution uses when a requested version is unavailable. Only one default per entry.
func (r *Registry) SetDefault(ctx context.Context, name, version string) error {
	e, ok, err := r.get(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("registry: %s not found", name)
	}
	idx := indexOf(e, version)
	if idx < 0 {
		return fmt.Errorf("registry: %s@%s not found", name, version)
	}
	if e.Versions[idx].Status != StatusPublished {
		return fmt.Errorf("registry: only a published version can be the default")
	}
	for i := range e.Versions {
		e.Versions[i].Default = false
	}
	e.Versions[idx].Default = true
	e.Versions[idx].UpdatedAt = r.now()
	return r.save(ctx, e)
}

// transition loads an entry, applies fn to the named version, then stamps and saves.
func (r *Registry) transition(ctx context.Context, name, version string, fn func(*Version) error) (Version, error) {
	e, ok, err := r.get(ctx, name)
	if err != nil {
		return Version{}, err
	}
	if !ok {
		return Version{}, fmt.Errorf("registry: %s not found", name)
	}
	idx := indexOf(e, version)
	if idx < 0 {
		return Version{}, fmt.Errorf("registry: %s@%s not found", name, version)
	}
	if err := fn(&e.Versions[idx]); err != nil {
		return Version{}, err
	}
	e.Versions[idx].UpdatedAt = r.now()
	if err := r.save(ctx, e); err != nil {
		return Version{}, err
	}
	return e.Versions[idx], nil
}
