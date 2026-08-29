package kafkagrant

import (
	"context"
	"errors"
	"fmt"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Adapter applies approved grants to one Kafka cluster.
//
// One public constructor, one collaborator: the [Admin] seam. Give a test the fake
// explicitly and every rule in this file is exercised without a broker.
type Adapter struct {
	admin Admin
	now   func() time.Time
}

// NewAdapter returns an Adapter over admin. Panics on a nil admin — an adapter with
// nothing to apply to is a wiring error, and failing at construction beats failing on
// the first governed request.
func NewAdapter(admin Admin) *Adapter {
	if admin == nil {
		panic("kafkagrant: NewAdapter requires a non-nil Admin")
	}
	return &Adapter{admin: admin, now: time.Now}
}

// OnboardTeam gives a team a namespace it owns: PREFIXED ACLs over the boundary's
// prefix, plus the caller-supplied byte-rate quota for its principal.
//
// This is the RARE, gated operation — the one a human approves. It is also the only one
// that widens what the team may do, which is why everything afterwards can be instant.
//
// Order matters: the quota is applied BEFORE the ACLs. The quota is the resource floor,
// and a principal that can produce before it can be throttled is a window — small, but a
// real one — in which one team can saturate the cluster. Bound first, then permit.
//
// The returned artifact is read back from the cluster.
func (a *Adapter) OnboardTeam(ctx context.Context, token mantlekeep.ExecutionToken, boundary Boundary) (BoundaryArtifact, error) {
	if err := a.checkToken(token); err != nil {
		return BoundaryArtifact{}, err
	}
	bindings, err := PlanBoundaryACLs(boundary)
	if err != nil {
		return BoundaryArtifact{}, err
	}
	alteration, err := PlanQuota(boundary)
	if err != nil {
		return BoundaryArtifact{}, err
	}

	if err := a.admin.AlterClientQuota(ctx, alteration); err != nil {
		return BoundaryArtifact{}, fmt.Errorf("kafkagrant: set quota for %s: %w", boundary.Principal, err)
	}
	if err := a.admin.CreateACLs(ctx, bindings); err != nil {
		return BoundaryArtifact{}, fmt.Errorf("kafkagrant: create acls for %s on %q: %w", boundary.Principal, boundary.Prefix, err)
	}

	return a.readBoundary(ctx, token, boundary)
}

// Provision creates ONE topic inside a namespace the team already owns.
//
// This is the FREQUENT, instant operation. It grants nothing: the PREFIXED ACL written
// at onboarding already covers every name under the prefix, so no permission changes
// here and nothing needs a fresh approval.
//
// It refuses a topic outside the granted prefix. The caller is expected to have checked
// too — this check is the second one, on purpose. Defence in depth is what stops a bug
// in the layer above from turning into a resource nobody approved.
//
// It is idempotent: a topic that already exists is success, not failure, so a retried or
// replayed grant converges instead of erroring.
func (a *Adapter) Provision(ctx context.Context, token mantlekeep.ExecutionToken, grant Grant) (TopicArtifact, error) {
	if err := a.checkToken(token); err != nil {
		return TopicArtifact{}, err
	}
	// Validate (which includes the boundary check) happens inside PlanTopic, before any
	// call reaches the cluster. A refusal must abort before the side effect, not after.
	spec, err := PlanTopic(grant)
	if err != nil {
		return TopicArtifact{}, err
	}

	alreadyExisted := false
	if err := a.admin.CreateTopic(ctx, spec); err != nil {
		if !errors.Is(err, ErrTopicExists) {
			return TopicArtifact{}, fmt.Errorf("kafkagrant: create topic %q: %w", grant.Topic, err)
		}
		alreadyExisted = true
	}

	config, err := a.admin.DescribeTopicConfig(ctx, grant.Topic)
	if err != nil {
		return TopicArtifact{}, fmt.Errorf("kafkagrant: read back topic %q: %w", grant.Topic, err)
	}
	return TopicArtifact{
		IntentID:       token.IntentID,
		PolicyID:       token.PolicyID,
		Topic:          grant.Topic,
		Config:         config,
		AlreadyExisted: alreadyExisted,
		ObservedAt:     a.now().UTC(),
	}, nil
}

// readBoundary asks the cluster what it now holds for the principal. Nothing in the
// returned artifact's cluster-state fields comes from the request.
func (a *Adapter) readBoundary(ctx context.Context, token mantlekeep.ExecutionToken, boundary Boundary) (BoundaryArtifact, error) {
	acls, err := a.admin.DescribeACLs(ctx, boundary.Principal, boundary.Prefix, kmsg.ACLResourcePatternTypePrefixed)
	if err != nil {
		return BoundaryArtifact{}, fmt.Errorf("kafkagrant: read back acls for %s: %w", boundary.Principal, err)
	}
	quota, err := a.admin.DescribeClientQuota(ctx, QuotaEntityTypeUser, boundary.Principal.Name())
	if err != nil {
		return BoundaryArtifact{}, fmt.Errorf("kafkagrant: read back quota for %s: %w", boundary.Principal, err)
	}
	return BoundaryArtifact{
		IntentID:   token.IntentID,
		PolicyID:   token.PolicyID,
		Principal:  boundary.Principal,
		Prefix:     boundary.Prefix,
		ACLs:       acls,
		Quota:      quota,
		ObservedAt: a.now().UTC(),
	}, nil
}

// checkToken refuses a request carrying no token or an expired one.
//
// Be clear about what this is and is not. The core states plainly that an
// ExecutionToken is unsigned and is EVIDENCE of a decision rather than a capability —
// this adapter cannot verify that a token it was handed was ever issued by a door, and
// a caller that never went through the door can fabricate one. So this check does not
// make an ungoverned apply impossible. What it does do is make an EXPIRED approval stop
// applying, and put the intent and policy that authorised the work onto the artifact so
// the cluster change can be tied back to a decision. The structural control is elsewhere:
// an executor that holds no cluster credentials until the door releases them.
func (a *Adapter) checkToken(token mantlekeep.ExecutionToken) error {
	if token.IntentID == "" {
		return fmt.Errorf("%w: no intent id — apply only under a token the door issued", ErrNotApproved)
	}
	if !token.Valid(a.now()) {
		return fmt.Errorf("%w: token for intent %s expired at %s", ErrNotApproved, token.IntentID, token.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return nil
}
