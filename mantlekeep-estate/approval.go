package estate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ApprovalState is where a pending change has got to.
type ApprovalState string

const (
	// ApprovalPending is waiting for a person. It is not an error and not a failure — it is the
	// normal outcome of a gated change, and a caller that treats it as either will teach people
	// to route around the gate.
	ApprovalPending ApprovalState = "pending"
	// ApprovalApproved has been signed off and applied.
	ApprovalApproved ApprovalState = "approved"
	// ApprovalDeclined was refused by a person, with a reason.
	ApprovalDeclined ApprovalState = "declined"
	// ApprovalExpired ran out of time. A request nobody acted on must not sit forever: a queue
	// of stale approvals is indistinguishable from a queue of live ones, and people stop
	// reading both.
	ApprovalExpired ApprovalState = "expired"
)

// Approval is one change waiting for a person, and the record of what they decided.
//
// It exists because the door could report "require_approval" and nothing could act on it. A
// system that can say a change needs a person but gives that person nowhere to stand has a gate
// in name only.
type Approval struct {
	ID   string `json:"id"`
	Team string `json:"team"`
	// Change is the RESOLVED change, stored rather than re-derived. Re-resolving at approval
	// time would apply whatever the manifest and floor say THEN, which is not what anybody
	// signed off.
	Change DesiredItem `json:"change"`
	// Requester is who asked. Kept because separation of duties is a rule about approval, and
	// the rule needs both names to compare.
	Requester string `json:"requester"`
	// RequiredRoles is who may sign off, in the door's words. A refusal that cannot say who
	// unblocks it is a dead end wearing the shape of a process.
	RequiredRoles []string `json:"requiredRoles,omitempty"`
	// FloorRevision is the floor this change was resolved and refused under. Checked again at
	// approval: the floor is hot-reloadable, so the rules can move between the request and the
	// signature, and an approval given under one set of limits must not apply under another.
	FloorRevision string `json:"floorRevision"`
	// Reason is the door's own words for why a person is needed.
	Reason string `json:"reason"`

	State          ApprovalState `json:"state"`
	ApprovedBy     string        `json:"approvedBy,omitempty"`
	DeclinedBy     string        `json:"declinedBy,omitempty"`
	DeclinedReason string        `json:"declinedReason,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	DecidedAt time.Time `json:"decidedAt,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Pending reports whether this still awaits a person, expiry included — because an expired
// request is not pending however its state field reads if nobody has swept it yet.
func (a Approval) Pending(now time.Time) bool {
	return a.State == ApprovalPending && now.Before(a.ExpiresAt)
}

// Errors a caller must be able to tell apart. Each is a different thing for a person to do.
var (
	// ErrApprovalNotFound — nothing to act on. Distinct from a declined one: "never existed"
	// and "was refused" send somebody looking in different places.
	ErrApprovalNotFound = errors.New("estate: no such approval")
	// ErrApprovalNotPending — already decided or expired. Approving twice must not apply twice.
	ErrApprovalNotPending = errors.New("estate: this approval has already been decided")
	// ErrSelfApproval — the requester cannot be the approver. THE floor: no configuration
	// reaches it, because a two-party rule satisfied by one party is not a rule.
	ErrSelfApproval = errors.New(
		"estate: the requester may not approve their own change — separation of duties is not " +
			"a policy this deployment can relax")
	// ErrWrongRole is NOT raised here. Whether an approver holds a required role is the door's
	// to answer from the directory — an estate that accepted roles from its caller would let a
	// caller assert its way past any gate. Kept as documentation of where that check lives.
	ErrWrongRole = errors.New(
		"estate: role checks belong to the door, which resolves them from the directory")
	// ErrFloorMoved — the floor changed between the request and the signature, so what would
	// be applied is not what was approved.
	ErrFloorMoved = errors.New(
		"estate: the floor has changed since this was requested — the limits that would now " +
			"apply are not the ones that were approved, so this must be requested again")
)

// Approvals stores changes awaiting a person.
//
// A port rather than a table: an approval outlives a process by definition — that is what makes
// it an approval rather than a prompt — so where it lives is a deployment's choice.
type Approvals interface {
	// Open records a new pending approval.
	Open(ctx context.Context, approval Approval) error
	// Get returns one approval, or ErrApprovalNotFound.
	Get(ctx context.Context, id string) (Approval, error)
	// Decide replaces an approval that is still pending. It must refuse to overwrite one that
	// has already been decided, or a race approves the same change twice.
	Decide(ctx context.Context, approval Approval) error
	// Pending lists what is waiting, for one team or — with an empty team — all of them, so a
	// platform approver can see the queue they are the gate for.
	Pending(ctx context.Context, team string) ([]Approval, error)
}

// approvalID names a pending change so a human can quote it and a caller can poll it.
func approvalID(team, name string, at time.Time) string {
	return fmt.Sprintf("APR-%s-%s-%d", team, name, at.UnixNano())
}
