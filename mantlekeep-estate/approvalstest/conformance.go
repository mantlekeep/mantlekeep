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
//
// A second rule joined it when a change could require a SET of distinct signatures. [estate.Signer]
// says its append is atomic and that the distinctness rules are checked inside the same critical
// section; both are exactly as unenforceable by a type system, and a store that loses one signature
// of two is indistinguishable from a correct one until a production change two people signed sits
// waiting for a third. The cases below prove that too, and they are SKIPPED with a reason on a store
// that does not implement the extension — which is honest, because such a store is not wrong, it is
// limited to one signature and the manager refuses to use it for more.
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

	// A required SET of distinct signatures. Everything below needs [estate.Signer].
	t.Run("a partial set stays pending and says who is still needed",
		func(t *testing.T) { partialSetStaysPending(t, newStore) })
	t.Run("the completing signature decides the record",
		func(t *testing.T) { lastSignatureDecides(t, newStore) })
	t.Run("the same person cannot fill two slots",
		func(t *testing.T) { noSignerTwice(t, newStore) })
	t.Run("the requester cannot fill a slot",
		func(t *testing.T) { noRequesterSignature(t, newStore) })
	t.Run("concurrent distinct signers fill EXACTLY the required number of slots",
		func(t *testing.T) { exactlyTheRequiredNumberWin(t, newStore) })
	t.Run("a record cannot be decided approved with signatures outstanding",
		func(t *testing.T) { noApprovalWithSignaturesOutstanding(t, newStore) })
	t.Run("a record written before signatures were a list still reads as approved",
		func(t *testing.T) { oldSingleSignatureRecordReadsBack(t, newStore) })
}

func pending(id, team string) estate.Approval {
	return estate.Approval{
		ID: id, Team: team, State: estate.ApprovalPending,
		Requester: "dev-alice", CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
}

// THE test for a DECISION. Many approvers race to decide one approval; exactly one may succeed.
//
// This is the property that makes an approval a GATE. If two succeed, the second-person rule has
// been satisfied by one person twice, or by two people who each believed they were deciding
// something still open — and the chain records both as valid.
//
// It is about Decide, and it still means exactly what it always did now that a change can require
// several signatures: one DECISION per record, whether that decision was reached by one signature
// or by five. The set arithmetic has its own case — exactlyTheRequiredNumberWin — because
// "exactly one decision" and "exactly N signatures" are two different races over the same record,
// and a store could get either one right while losing the other.
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
