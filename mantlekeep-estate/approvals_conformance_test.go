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

// A store written before [estate.Signer] existed must STILL pass the suite.
//
// This is the ABI promise, asserted. [estate.Approvals] is published: a store somebody compiled
// against it does not gain a method because this package did, and it must not be told it no
// longer conforms. The multi-signature cases skip against it — with a message naming the limit —
// and everything the published contract ever promised still has to hold.
//
// The limit itself is not left implicit: see
// TestAStoreThatCannotAppendAtomicallyIsRefusedForASetAndStillServesOne, where the manager
// refuses a change requiring several signatures against such a store rather than losing one.
func TestAStoreWithoutTheSignerExtensionStillConforms(t *testing.T) {
	approvalstest.Run(t, func(t *testing.T) estate.Approvals {
		return publishedInterfaceOnly{inner: estate.NewMemoryApprovals()}
	})
}

// publishedInterfaceOnly exposes nothing but the four published methods, so a type assertion to
// [estate.Signer] fails exactly as it would on a store from before the extension.
type publishedInterfaceOnly struct{ inner *estate.MemoryApprovals }

func (s publishedInterfaceOnly) Open(ctx context.Context, approval estate.Approval) error {
	return s.inner.Open(ctx, approval)
}

func (s publishedInterfaceOnly) Get(ctx context.Context, id string) (estate.Approval, error) {
	return s.inner.Get(ctx, id)
}

func (s publishedInterfaceOnly) Decide(ctx context.Context, approval estate.Approval) error {
	return s.inner.Decide(ctx, approval)
}

func (s publishedInterfaceOnly) Pending(ctx context.Context, team string) ([]estate.Approval, error) {
	return s.inner.Pending(ctx, team)
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

// The same deployment limit, restated for a required SET — because a set makes it WORSE, and the
// old test would keep passing while saying nothing about it.
//
// MemoryApprovals.Sign is atomic within one process, which is what makes "exactly N distinct
// signatures" true. Two replicas hold two locks over two maps: each collects its own full set,
// so a change requiring two signatures is applied TWICE, under four signatures, and each replica's
// record reads as a correct two-person approval. Nothing in either record shows the other.
//
// Written as a PASSING test of the broken behaviour, for the same reason as the case above: the
// constraint should be a failing test away from being noticed, not a line in a Helm chart somebody
// raises to 2 on a busy afternoon.
func TestTwoStoresEachCollectTheirOwnSignatureSet(t *testing.T) {
	replicas := []*estate.MemoryApprovals{estate.NewMemoryApprovals(), estate.NewMemoryApprovals()}
	ctx := context.Background()

	waiting := estate.Approval{
		ID: "AP-SPLIT-SET", Team: "payments", State: estate.ApprovalPending,
		Requester: "dev-alice", RequiredSignatures: 2,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	for _, replica := range replicas {
		if err := replica.Open(ctx, waiting); err != nil {
			t.Fatalf("opening: %v", err)
		}
	}

	// Four distinct approvers, two landing on each replica — as they would through a load
	// balancer with no session affinity and no shared store.
	signers := [][]string{{"lead-bob", "arch-carol"}, {"sec-dave", "ops-erin"}}
	completed := 0
	for index, replica := range replicas {
		for _, who := range signers[index] {
			after, err := replica.Sign(ctx, waiting.ID,
				estate.Signature{By: who, At: time.Now().UTC()})
			if err != nil {
				t.Fatalf("%s signing on replica %d: %v", who, index, err)
			}
			if after.State == estate.ApprovalApproved {
				completed++
			}
		}
	}

	if completed != 2 {
		t.Fatalf("expected BOTH replicas to complete their own set — that is the bug being "+
			"documented; got %d", completed)
	}
	t.Log("a change requiring 2 signatures collected 4, completed twice, and each replica's " +
		"record reads as a correct two-person approval with no trace of the other. A required " +
		"set is only as distinct as the store that serialises it: estate runs a single replica " +
		"until a store with compare-and-set — and an atomic Sign — backs it.")
}
