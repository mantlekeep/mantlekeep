package estate

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryApprovals keeps pending approvals in memory.
//
// HONEST about what it is: a restart forgets every pending change, so a person who was asked to
// sign something off will find it gone. That is unacceptable in a deployment and fine for a
// demo — which is why the service logs what it is running with rather than letting an operator
// discover it from behaviour.
type MemoryApprovals struct {
	mu sync.RWMutex
	by map[string]Approval
	// now is injectable so a test can drive expiry without sleeping.
	now func() time.Time
}

// NewMemoryApprovals returns an empty in-memory store.
func NewMemoryApprovals() *MemoryApprovals {
	return &MemoryApprovals{by: map[string]Approval{}, now: time.Now}
}

var (
	_ Approvals = (*MemoryApprovals)(nil)
	_ Signer    = (*MemoryApprovals)(nil)
)

// Open records a pending approval.
func (m *MemoryApprovals) Open(_ context.Context, approval Approval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Signatures copied on the way IN. The caller still holds the slice it passed; storing it
	// directly would let the caller append to the stored record afterwards, outside this lock.
	approval.Signatures = cloneSignatures(approval.Signatures)
	m.by[approval.ID] = approval
	return nil
}

// Get returns one approval, marking it expired on read if its time has passed.
//
// Expiry is evaluated on READ rather than by a sweeper, so a store with no background loop still
// cannot hand back a stale approval as pending. A sweeper can exist as an optimisation; it must
// not be what makes the rule true.
func (m *MemoryApprovals) Get(_ context.Context, id string) (Approval, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	approval, ok := m.by[id]
	if !ok {
		return Approval{}, ErrApprovalNotFound
	}
	if approval.State == ApprovalPending && !m.now().Before(approval.ExpiresAt) {
		approval.State = ApprovalExpired
	}
	// And copied on the way OUT, for the reason [cloneSignatures] gives: the struct is a copy
	// but its slice field is not, so a reader that appended to it would write into the stored
	// record with no lock held.
	approval.Signatures = cloneSignatures(approval.Signatures)
	return approval, nil
}

// Decide replaces an approval, refusing to overwrite one already decided.
//
// The check is inside the lock on purpose: two approvers clicking at once would otherwise both
// read "pending", both write, and the change would be applied twice under two signatures.
func (m *MemoryApprovals) Decide(_ context.Context, approval Approval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.by[approval.ID]
	if !ok {
		return ErrApprovalNotFound
	}
	if !current.Pending(m.now()) {
		return ErrApprovalNotPending
	}
	// A decision may not mark a record APPROVED while its signature set is incomplete.
	//
	// Without this the whole N-signature requirement is one direct Decide call away from being
	// decoration: a caller writes state=approved with one of two signatures collected and the
	// record reads as signed off by a set that never assembled. Refused by the STORE, because
	// the store is what every caller has to go through. A DECLINE is not guarded — refusing a
	// change is a decision one person is entitled to make however many have signed, and it
	// keeps the signatures it had collected so the record shows both.
	if approval.State == ApprovalApproved && approval.SignaturesOutstanding() > 0 {
		return fmt.Errorf("%w (%s)", ErrSignaturesOutstanding, approval.StillNeeded())
	}
	approval.Signatures = cloneSignatures(approval.Signatures)
	m.by[approval.ID] = approval
	return nil
}

// Sign appends ONE signature and decides the record when the set is complete.
//
// Everything here happens under ONE write lock, and that is the whole contract. The distinctness
// rules are checked in the same critical section as the append because that is the only place
// they can be TRUE: two approvers signing in the same instant both read a record without the
// other's signature, and a check made before taking the lock would pass for both.
//
// The rules checked here are FLOORS, not policy — no configuration reaches them:
//
//   - the requester may not sign. A two-party rule satisfied by one party is not a rule, and
//     separation of duties is not a policy this deployment can relax.
//   - nobody may sign twice. N signatures from one person is one signature, so a required set
//     that allowed a repeat would be a count of clicks rather than a count of people.
//
// They are enforced HERE as well as in [Manager.Approve] deliberately. With several signatures
// the door is submitted to ONCE, by whoever completes the set, so every earlier signature is
// never seen by the door's own separation-of-duties rule. The estate is the only thing that
// sees them all, and the store is the only thing that sees them serialised.
//
// An AI is NOT refused here: this method is given a name, not a subject, and cannot know. That
// check belongs where the subject is — see [ErrAIApproval] and [Manager.Approve].
func (m *MemoryApprovals) Sign(_ context.Context, id string, signature Signature) (Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	approval, ok := m.by[id]
	if !ok {
		return Approval{}, ErrApprovalNotFound
	}
	// Already decided, or expired, or the set was completed by somebody a microsecond ago. This
	// single check is what makes N+1 signatures impossible: the record leaves the pending state
	// on the completing signature, and every later attempt lands here.
	if !approval.Pending(m.now()) {
		return Approval{}, ErrApprovalNotPending
	}
	if signature.By == approval.Requester {
		return Approval{}, ErrSelfApproval
	}
	if approval.HasSignatureFrom(signature.By) {
		return Approval{}, ErrDuplicateSignature
	}

	// Copy before appending. approval is a struct copy, but its Signatures slice still points
	// at the stored backing array, and appending in place could write through it.
	approval.Signatures = append(cloneSignatures(approval.Signatures), signature)
	if approval.SignaturesOutstanding() == 0 {
		// Decided HERE, on the last signature, and before the manager applies anything — the
		// same ordering [Manager.Approve] relies on, so a crash between the two leaves a
		// signed-and-unapplied change rather than an applied change nobody signed.
		approval.State = ApprovalApproved
		approval.ApprovedBy = signature.By
		approval.DecidedAt = signature.At
	}
	m.by[id] = approval
	// The returned record gets its own copy too: the caller is about to read it outside this
	// lock while other signers are writing.
	approval.Signatures = cloneSignatures(approval.Signatures)
	return approval, nil
}

// Pending lists what is waiting, newest first so a queue reads as a queue.
func (m *MemoryApprovals) Pending(_ context.Context, team string) ([]Approval, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.now()
	waiting := make([]Approval, 0, len(m.by))
	for _, approval := range m.by {
		if !approval.Pending(now) {
			continue
		}
		if team != "" && approval.Team != team {
			continue
		}
		approval.Signatures = cloneSignatures(approval.Signatures)
		waiting = append(waiting, approval)
	}
	sort.Slice(waiting, func(i, j int) bool {
		return waiting[i].CreatedAt.After(waiting[j].CreatedAt)
	})
	return waiting, nil
}
