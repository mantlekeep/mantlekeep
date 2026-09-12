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
	// RequiredSignatures is how many DISTINCT people must sign before the change is applied.
	//
	// A SET, not a sequence: the required signatures have no order, no stages and no turns.
	// Whichever people satisfy [Approval.RequiredRoles] may sign in any order, and the set is
	// complete when N of them have. See NOTES.md for why ordering is deliberately absent.
	//
	// Resolved from the floor when the request is opened and STORED, exactly as the resolved
	// change and the floor revision are: re-reading the count at approval time would let a
	// config edit turn a change two people already signed into one needing three, and nobody
	// would have signed off on that requirement.
	//
	// ZERO means ONE — read it through [Approval.SignaturesRequired], never directly. Every
	// record written before this field existed meant "one person must sign", and a zero taken
	// literally would silently turn those into changes needing nobody.
	RequiredSignatures int `json:"requiredSignatures,omitempty"`
	// FloorRevision is the floor this change was resolved and refused under. Checked again at
	// approval: the floor is hot-reloadable, so the rules can move between the request and the
	// signature, and an approval given under one set of limits must not apply under another.
	FloorRevision string `json:"floorRevision"`
	// Reason is the door's own words for why a person is needed.
	Reason string `json:"reason"`

	State ApprovalState `json:"state"`
	// ApprovedBy is the COMPLETING signature — the person whose signature finished the set and
	// released the change.
	//
	// It is deliberately not repurposed and deliberately not removed, because it is persisted
	// and every record already written uses it to mean "the one person who approved this". The
	// completing signature is the defensible reading of that field once there are several: it
	// is the signature the change was applied ON, it is what the door was submitted as, and it
	// is the name on the chain entry for the effect. A reader wanting the whole set reads
	// [Approval.Signatures] (or [Approval.CollectedSignatures], which also reads an old record
	// correctly); a reader wanting "who released it" reads this. What this field must never be
	// read as is "the only person who approved".
	ApprovedBy string `json:"approvedBy,omitempty"`
	// Signatures is every sign-off collected, in the order it arrived.
	//
	// Order of ARRIVAL is a fact worth keeping; it is not an order of precedence and nothing
	// reads it to decide who may sign next. Kept on a DECLINED record too, so a change one
	// person signed and another refused shows both — a declined record that dropped its
	// signatures would read as though nobody had ever agreed.
	Signatures     []Signature `json:"signatures,omitempty"`
	DeclinedBy     string      `json:"declinedBy,omitempty"`
	DeclinedReason string      `json:"declinedReason,omitempty"`

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

// Signer is the OPTIONAL extension of [Approvals] that collects ONE signature at a time.
//
// A separate interface rather than a fifth method on [Approvals], because [Approvals] is
// published: adding a method to it would stop every store already compiled against it from
// building, and a port may be extended but never narrowed. A store that does not implement this
// keeps working exactly as it did and can serve any change needing ONE signature;
// [Manager.Approve] refuses a change needing several against it, naming the store, rather than
// collecting signatures it cannot keep.
//
// Sign appends one signature and returns the record AS IT NOW STANDS, so a caller learns in one
// round trip whether the set is complete. Two obligations on an implementation, and both are
// proved by [approvalstest.Run] rather than reviewed for:
//
//   - It is ATOMIC, and every distinctness rule is checked INSIDE the same critical section.
//     Two approvers signing in the same instant both read the same record; a check outside the
//     lock lets each write a record that does not contain the other's signature, one signature
//     is silently lost, and a change two people signed waits forever for a third.
//   - It DECIDES the record when the last signature lands — state approved, ApprovedBy set to
//     the completing signer — and refuses every later signature as not-pending. That is what
//     makes N+1 signatures structurally impossible rather than merely unlikely.
type Signer interface {
	Sign(ctx context.Context, id string, signature Signature) (Approval, error)
}

// approvalID names a pending change so a human can quote it and a caller can poll it.
func approvalID(team, name string, at time.Time) string {
	return fmt.Sprintf("APR-%s-%s-%d", team, name, at.UnixNano())
}
