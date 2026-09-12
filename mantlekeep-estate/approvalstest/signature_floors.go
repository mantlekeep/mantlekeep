// This file is one concern: the conformance cases for what can never fill a slot in a required
// set, and for what happens when several people try to fill the remaining slots at once.
//
// Split from signatures.go, which proves a set can be COLLECTED and read. These prove what can
// never be collected — the requester, a repeat signer — and that the count is exact under
// concurrency. A store can get either half right while losing the other.
package approvalstest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// Distinctness, first floor: N signatures from one person is ONE signature.
//
// Checked inside the store's lock and not only by its caller, because the caller is not the only
// thing that reaches a store and a race is not the only way this is attempted.
func noSignerTwice(t *testing.T, newStore Factory) {
	store := newStore(t)
	signer := signerOrSkip(t, store)
	ctx := context.Background()

	if err := store.Open(ctx, needing("AP-TWICE", "payments", 2)); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	if _, err := signer.Sign(ctx, "AP-TWICE", signedBy("lead-bob")); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	_, err := signer.Sign(ctx, "AP-TWICE", signedBy("lead-bob"))
	if !errors.Is(err, estate.ErrDuplicateSignature) {
		t.Fatalf("one person filled two slots of a two-person set; err = %v — a required set "+
			"that accepts a repeat is counting clicks, not people", err)
	}
	found, _ := store.Get(ctx, "AP-TWICE")
	if len(found.Signatories()) != 1 || found.State != estate.ApprovalPending {
		t.Fatalf("record holds %v in state %q — the refused repeat must leave the request "+
			"waiting for somebody else", found.Signatories(), found.State)
	}
}

// Distinctness, second floor: the requester may never fill a slot, however many there are.
//
// A set of N signatures that the requester can contribute one of is a set of N-1 signatures with
// extra ceremony, and nothing in a document can relax this.
func noRequesterSignature(t *testing.T, newStore Factory) {
	store := newStore(t)
	signer := signerOrSkip(t, store)
	ctx := context.Background()

	waiting := needing("AP-SELF", "payments", 2)
	if err := store.Open(ctx, waiting); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	_, err := signer.Sign(ctx, "AP-SELF", signedBy(waiting.Requester))
	if !errors.Is(err, estate.ErrSelfApproval) {
		t.Fatalf("the requester signed their own change; err = %v — separation of duties is "+
			"not a policy a deployment can relax", err)
	}
	found, _ := store.Get(ctx, "AP-SELF")
	if len(found.Signatories()) != 0 {
		t.Fatalf("the requester's signature was recorded anyway: %v", found.Signatories())
	}
}

// THE test for a SET. Many distinct approvers race; EXACTLY the required number may win.
//
// This is the multi-signature counterpart of onlyOneWins, and it is a different property. Under
// one decision the answer is "one wins"; under a required set of three, three DIFFERENT people
// may each take a slot concurrently — and the store must produce exactly three, never four, never
// two with one signature silently dropped. Both failures are invisible in production: four means
// a slot was filled twice, and two means a change that three people signed waits forever while
// the record says it needs one more.
func exactlyTheRequiredNumberWin(t *testing.T, newStore Factory) {
	store := newStore(t)
	signer := signerOrSkip(t, store)
	ctx := context.Background()

	const (
		approvers = 16
		required  = 3
	)
	if err := store.Open(ctx, needing("AP-SET-RACE", "payments", required)); err != nil {
		t.Fatalf(wrapOpening, err)
	}

	var (
		start    = make(chan struct{})
		wait     sync.WaitGroup
		mu       sync.Mutex
		accepted []string
	)
	for approver := 0; approver < approvers; approver++ {
		wait.Add(1)
		go func(n int) {
			defer wait.Done()
			<-start // released together, so the window is as narrow as the store allows
			who := fmt.Sprintf("approver-%d", n)
			if _, err := signer.Sign(ctx, "AP-SET-RACE", signedBy(who)); err == nil {
				mu.Lock()
				accepted = append(accepted, who)
				mu.Unlock()
			}
		}(approver)
	}
	close(start)
	wait.Wait()

	if len(accepted) != required {
		t.Fatalf("%d of %d distinct signers were accepted, want exactly %d — more means a "+
			"slot was filled twice, fewer means a signature was read, written and lost",
			len(accepted), approvers, required)
	}
	found, err := store.Get(ctx, "AP-SET-RACE")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if found.State != estate.ApprovalApproved {
		t.Fatalf("state = %q with every slot filled — %s", found.State, found.StillNeeded())
	}
	recorded := found.Signatories()
	if len(recorded) != required {
		t.Fatalf("the record holds %d signatures (%v) after %d accepted signers — a signature "+
			"that was accepted and not stored is the worst of both answers", len(recorded),
			recorded, len(accepted))
	}
	seen := map[string]bool{}
	for _, who := range recorded {
		if seen[who] {
			t.Fatalf("%q appears twice in %v — one person filled two slots under concurrency, "+
				"which is exactly what a check outside the lock allows", who, recorded)
		}
		seen[who] = true
	}

	// And one more signature is impossible, not merely unlikely: the record left the pending
	// state on the completing signature.
	if _, err := signer.Sign(ctx, "AP-SET-RACE", signedBy("approver-late")); !errors.Is(err,
		estate.ErrApprovalNotPending) {
		t.Fatalf("a signature was accepted after the set completed; err = %v — that is the "+
			"N+1st signature on a change already applied under N", err)
	}
}

// A store must refuse to mark a record APPROVED while signatures are outstanding.
//
// Otherwise the whole requirement is one direct Decide call away from being decoration: a caller
// writes the state it wanted, and the record reads as signed off by a set that never assembled.
func noApprovalWithSignaturesOutstanding(t *testing.T, newStore Factory) {
	store := newStore(t)
	signerOrSkip(t, store)
	ctx := context.Background()

	if err := store.Open(ctx, needing("AP-SHORTCUT", "payments", 2)); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	shortcut := needing("AP-SHORTCUT", "payments", 2)
	shortcut.State = estate.ApprovalApproved
	shortcut.ApprovedBy = "lead-bob"
	shortcut.Signatures = []estate.Signature{signedBy("lead-bob")}
	if err := store.Decide(ctx, shortcut); !errors.Is(err, estate.ErrSignaturesOutstanding) {
		t.Fatalf("a record was approved with 1 of 2 signatures; err = %v", err)
	}

	// A DECLINE is not guarded, and must not be: refusing a change is a decision one person is
	// entitled to make however many people were required to agree.
	declined := needing("AP-SHORTCUT", "payments", 2)
	declined.State = estate.ApprovalDeclined
	declined.DeclinedBy = "lead-bob"
	declined.DeclinedReason = "the rollback plan does not cover the schema change"
	declined.Signatures = []estate.Signature{signedBy("lead-bob")}
	if err := store.Decide(ctx, declined); err != nil {
		t.Fatalf("a decline on a partially signed change must be accepted: %v", err)
	}
	found, _ := store.Get(ctx, "AP-SHORTCUT")
	if len(found.Signatories()) != 1 {
		t.Errorf("the decline dropped the signature already collected (%v) — a change one "+
			"person signed and another refused must read as both", found.Signatories())
	}
}
