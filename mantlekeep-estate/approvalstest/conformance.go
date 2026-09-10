// Package approvalstest is the conformance suite every Approvals implementation must pass.
//
// The Approvals contract carries a rule that a type system cannot express: Decide "must refuse to
// overwrite one that has already been decided, or a race approves the same change twice." A
// comment cannot enforce that, and a store that quietly loses the race is indistinguishable from
// one that wins it — until two people approve the same production change and both believe they
// were the only one.
//
// So the rule ships as a runnable suite. A new store — Postgres, etcd, anything — imports this and
// proves it, rather than being reviewed for it.
package approvalstest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// wrapOpening is the one message used wherever opening an approval fails, so the string exists
// once rather than in every case that opens one.
const wrapOpening = "opening: %v"

// Factory builds a fresh, empty store. Called once per sub-test so cases cannot leak into
// each other — a suite whose cases share state passes for reasons nobody can name.
type Factory func(t *testing.T) estate.Approvals

// Run executes the whole suite against one implementation.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	t.Run("a pending approval can be read back", func(t *testing.T) { readBack(t, newStore) })
	t.Run("only ONE decision wins under concurrency", func(t *testing.T) { onlyOneWins(t, newStore) })
	t.Run("a decided approval cannot be decided again", func(t *testing.T) { noSecondDecision(t, newStore) })
	t.Run("pending lists what is waiting", func(t *testing.T) { listsPending(t, newStore) })
}

func pending(id, team string) estate.Approval {
	return estate.Approval{
		ID: id, Team: team, State: estate.ApprovalPending,
		Requester: "dev-alice", CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
}

func readBack(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Open(ctx, pending("AP-1", "payments")); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	found, err := store.Get(ctx, "AP-1")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if found.State != estate.ApprovalPending {
		t.Fatalf("a freshly opened approval is pending, got %q", found.State)
	}
}

// THE test. Many approvers race to decide one approval; exactly one may succeed.
//
// This is the property that makes an approval a GATE. If two succeed, the second-person rule has
// been satisfied by one person twice, or by two people who each believed they were deciding
// something still open — and the chain records both as valid.
func onlyOneWins(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Open(ctx, pending("AP-RACE", "payments")); err != nil {
		t.Fatalf(wrapOpening, err)
	}

	const approvers = 16
	var (
		start    = make(chan struct{})
		wait     sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)
	for approver := 0; approver < approvers; approver++ {
		wait.Add(1)
		go func(n int) {
			defer wait.Done()
			<-start // release them together, so the window is as narrow as the store allows
			decided := pending("AP-RACE", "payments")
			decided.State = estate.ApprovalApproved
			decided.ApprovedBy = fmt.Sprintf("approver-%d", n)
			if store.Decide(ctx, decided) == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(approver)
	}
	close(start)
	wait.Wait()

	if accepted != 1 {
		t.Fatalf("exactly one decision may win; %d of %d were accepted — the same change was "+
			"approved more than once", accepted, approvers)
	}
}

func noSecondDecision(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Open(ctx, pending("AP-2", "payments")); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	approved := pending("AP-2", "payments")
	approved.State = estate.ApprovalApproved
	approved.ApprovedBy = "lead-bob"
	if err := store.Decide(ctx, approved); err != nil {
		t.Fatalf("the first decision must be accepted: %v", err)
	}

	// A second decision, even a different one, must be refused. Reversing a decision is a NEW
	// governed act with its own record — never an overwrite that leaves no trace of the first.
	declined := pending("AP-2", "payments")
	declined.State = estate.ApprovalDeclined
	declined.DeclinedBy = "lead-carol"
	if store.Decide(ctx, declined) == nil {
		t.Fatal("a decided approval was decided again — the first decision vanished silently")
	}
}

func listsPending(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	for _, spec := range []struct{ id, team string }{
		{"AP-A", "payments"}, {"AP-B", "payments"}, {"AP-C", "treasury"},
	} {
		if err := store.Open(ctx, pending(spec.id, spec.team)); err != nil {
			t.Fatalf("opening %s: %v", spec.id, err)
		}
	}

	forTeam, err := store.Pending(ctx, "payments")
	if err != nil {
		t.Fatalf("listing one team: %v", err)
	}
	if len(forTeam) != 2 {
		t.Fatalf("payments has 2 waiting, got %d", len(forTeam))
	}

	// An empty team means ALL of them — the queue a platform approver is the gate for.
	all, err := store.Pending(ctx, "")
	if err != nil {
		t.Fatalf("listing every team: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("three approvals are waiting overall, got %d", len(all))
	}

	// A decided one leaves the queue, or an approver keeps seeing work that is done.
	approved := pending("AP-A", "payments")
	approved.State = estate.ApprovalApproved
	approved.ApprovedBy = "lead-bob"
	if err := store.Decide(ctx, approved); err != nil {
		t.Fatalf("deciding: %v", err)
	}
	forTeam, err = store.Pending(ctx, "payments")
	if err != nil || len(forTeam) != 1 {
		t.Fatalf("a decided approval must leave the queue: %d remain, err=%v", len(forTeam), err)
	}
}

// ErrAlreadyDecided is what a conforming store should wrap when Decide is refused, so a caller can
// branch without matching on a message. Exported here because the suite is the contract's home.
var ErrAlreadyDecided = errors.New("approval has already been decided")
