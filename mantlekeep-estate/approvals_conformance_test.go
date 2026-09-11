package estate_test

import (
	"context"
	"sync"
	"testing"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/approvalstest"
)

// The shipped in-memory store must obey the contract it implements.
func TestMemoryApprovalsConformsToTheContract(t *testing.T) {
	approvalstest.Run(t, func(t *testing.T) estate.Approvals {
		return estate.NewMemoryApprovals()
	})
}

// And the limit of that store, asserted rather than left in a comment.
//
// MemoryApprovals guards Decide with a mutex, which is correct and sufficient inside ONE process.
// Two replicas hold two mutexes over two maps and coordinate nothing, so both accept a decision on
// the same approval and each believes it was the only one — the second-person rule satisfied twice.
//
// This test creates two stores deliberately, because that is what a second replica IS. It exists
// so the deployment constraint is a failing test away from being noticed, rather than a line in a
// Helm chart that somebody raises to 2 on a busy afternoon.
func TestTwoStoresCannotCoordinate(t *testing.T) {
	replicaOne := estate.NewMemoryApprovals()
	replicaTwo := estate.NewMemoryApprovals()
	ctx := context.Background()

	waiting := estate.Approval{
		ID: "AP-SPLIT", Team: "payments", State: estate.ApprovalPending,
		Requester: "dev-alice", CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	// The same approval reaches both replicas — as it would through a load balancer.
	for _, replica := range []estate.Approvals{replicaOne, replicaTwo} {
		if err := replica.Open(ctx, waiting); err != nil {
			t.Fatalf("opening: %v", err)
		}
	}

	var (
		mu       sync.Mutex
		accepted int
		wait     sync.WaitGroup
	)
	for index, replica := range []estate.Approvals{replicaOne, replicaTwo} {
		wait.Add(1)
		go func(n int, store estate.Approvals) {
			defer wait.Done()
			decided := waiting
			decided.State = estate.ApprovalApproved
			decided.ApprovedBy = []string{"lead-bob", "lead-carol"}[n]
			if store.Decide(ctx, decided) == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(index, replica)
	}
	wait.Wait()

	if accepted != 2 {
		t.Fatalf("expected BOTH replicas to accept — that is the bug being documented; got %d", accepted)
	}
	t.Log("two replicas each accepted a decision on the same approval: the second-person rule " +
		"was satisfied twice. This is why estate runs a single replica until a store with " +
		"compare-and-set backs it.")
}
