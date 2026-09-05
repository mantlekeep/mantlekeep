package estate

import (
	"context"
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

var _ Approvals = (*MemoryApprovals)(nil)

// Open records a pending approval.
func (m *MemoryApprovals) Open(_ context.Context, approval Approval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.by[approval.ID] = approval
	return nil
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
		waiting = append(waiting, approval)
	}
	sort.Slice(waiting, func(i, j int) bool {
		return waiting[i].CreatedAt.After(waiting[j].CreatedAt)
	})
	return waiting, nil
}
