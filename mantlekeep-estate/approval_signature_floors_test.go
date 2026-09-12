package estate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// This file is one concern: what can NEVER fill a slot in a required set, and what happens when
// several people try to fill the remaining slots at the same instant.
//
// Every rule here is a FLOOR — enforced in code, reachable by no configuration — and each one is
// proved against a door that WOULD have allowed the change, so the guarantee does not depend on
// policy data, on the door being correct, or on a caller behaving.
//
// They matter more with a set than they ever did with a single signature, for one structural
// reason worth stating plainly: with N signatures the door is submitted to ONCE, by whoever
// completes the set. Every signature before the last is never shown to the door's own
// separation-of-duties rule or to the core's "an AI may never approve" rule. The estate is the
// only thing that sees them all.

// The requester may not fill a slot, however many slots there are.
//
// A set of N signatures the requester can contribute one of is a set of N-1 with extra ceremony.
func TestTheRequesterCannotFillASlotInARequiredSet(t *testing.T) {
	manager, door, store, port := signatureManager(t, 2)
	id := openGatedChange(t, manager)
	submissions := len(door.submitted)

	_, err := manager.Approve(context.Background(), mantlekeep.Subject{ID: "dev-alice"}, id)
	if !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("the requester signed their own change; got %v — a rule satisfied by the "+
			"party it exists to separate from is not a rule", err)
	}
	if len(door.submitted) != submissions {
		t.Error("a self-signature reached the door — the floor must not depend on policy data")
	}
	if len(port.tokens) != 0 {
		t.Fatal("the change reached the adapter on a self-signature")
	}
	still, err := store.Get(context.Background(), id)
	if err != nil || len(still.Signatories()) != 0 {
		t.Fatalf("the requester's signature was recorded anyway: %v (err=%v)",
			still.Signatories(), err)
	}
	if still.State != ApprovalPending {
		t.Errorf("state = %q — a refused self-signature must leave the request waiting for "+
			"the people who may actually sign it", still.State)
	}
}

// Nobody fills two slots. A change requiring two signatures requires two PEOPLE.
func TestTheSamePersonCannotFillTwoSlots(t *testing.T) {
	manager, _, store, port := signatureManager(t, 2)
	id := openGatedChange(t, manager)
	ctx := context.Background()
	keen := mantlekeep.Subject{ID: "lead-bob"}

	if _, err := manager.Approve(ctx, keen, id); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	_, err := manager.Approve(ctx, keen, id)
	if !errors.Is(err, ErrDuplicateSignature) {
		t.Fatalf("one person filled both slots of a two-person set; got %v — a required set "+
			"that accepts a repeat is counting clicks, not people", err)
	}
	if !strings.Contains(err.Error(), "DISTINCT") {
		t.Errorf("the error does not tell the reader what the rule is: %v", err)
	}
	if len(port.tokens) != 0 {
		t.Fatal("the change was applied on one person's two signatures")
	}
	recorded, _ := store.Get(ctx, id)
	if len(recorded.Signatories()) != 1 || recorded.State != ApprovalPending {
		t.Fatalf("record holds %v in state %q, want one signature and still waiting",
			recorded.Signatories(), recorded.State)
	}
	// And the request is still finishable by somebody else — a refused repeat must not have
	// burnt the slot it was refused from.
	result, err := manager.Approve(ctx, mantlekeep.Subject{ID: "arch-carol"}, id)
	if err != nil || !result.Applied() {
		t.Fatalf("a second, distinct person could not finish the set: err=%v result=%+v",
			err, result)
	}
}

// THE anti-routing test. An AI may never fill a slot, and requiring several signatures must not
// become the way around the core's refusal.
//
// The core denies an AI an approval ACTION at the door. With a required set the door is asked
// ONCE, by whoever completes the set — so an AI taking the FIRST of two slots would never reach
// that rule at all, and the change would apply with a robot's name in the signature set and a
// human's name on the submission. That is why this is refused in the estate, in code, before the
// store is touched.
func TestAnAIMayNeverFillASlotEvenWhenAHumanCompletesTheSet(t *testing.T) {
	manager, door, store, port := signatureManager(t, 2)
	id := openGatedChange(t, manager)
	ctx := context.Background()
	submissions := len(door.submitted)

	robot := mantlekeep.Subject{ID: "ci-agent", IsAI: true,
		Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}}
	_, err := manager.Approve(ctx, robot, id)
	if !errors.Is(err, ErrAIApproval) {
		t.Fatalf("an AI filled a slot in a required set; got %v — two signatures where one is "+
			"an automation is one signature with a witness", err)
	}
	if len(door.submitted) != submissions {
		t.Error("an AI signature reached the door — this floor must hold without the door")
	}
	recorded, _ := store.Get(ctx, id)
	if len(recorded.Signatories()) != 0 {
		t.Fatalf("the AI's signature was recorded: %v", recorded.Signatories())
	}

	// And it is not recoverable by having a human finish afterwards: the set must be two
	// PEOPLE, so two humans are still required and the AI's attempt bought nothing.
	if _, err := manager.Approve(ctx, mantlekeep.Subject{ID: "lead-bob"}, id); err != nil {
		t.Fatalf("first human signature: %v", err)
	}
	partial, _ := store.Get(ctx, id)
	if partial.SignaturesOutstanding() != 1 {
		t.Fatalf("outstanding = %d after one human signature — the AI's refused attempt must "+
			"not have counted toward the set", partial.SignaturesOutstanding())
	}
	if len(port.tokens) != 0 {
		t.Fatal("the change applied with an AI making up half of a two-person set")
	}
	result, err := manager.Approve(ctx, mantlekeep.Subject{ID: "arch-carol"}, id)
	if err != nil || !result.Applied() {
		t.Fatalf("two humans could not complete the set: err=%v result=%+v", err, result)
	}
}

// Concurrency, at the manager rather than the store: several DISTINCT people sign the same
// change at the same instant.
//
// The contract has three parts and each fails silently in production:
//   - every distinct signer who takes a slot is RECORDED. A signature read, written and lost
//     leaves a change three people signed waiting for a fourth, and nothing says why.
//   - never more than the required number. One more is a slot filled twice.
//   - the change is applied ONCE, by the one submission that completed the set.
func TestConcurrentDistinctSignersFillExactlyTheRequiredSlotsAndApplyOnce(t *testing.T) {
	const (
		approvers = 16
		required  = 3
	)
	manager, door, store, port := signatureManager(t, required)
	id := openGatedChange(t, manager)
	submissionsAtRequest := len(door.submitted)

	signed, applied := signAllAtOnce(manager, id, approvers)

	if len(signed) != required {
		t.Fatalf("%d of %d distinct approvers were accepted, want exactly %d — more means a "+
			"slot was filled twice, fewer means an accepted signature was lost",
			len(signed), approvers, required)
	}
	if applied != 1 {
		t.Fatalf("%d callers were told the change had been applied, want exactly 1 — the "+
			"change is applied by the submission that COMPLETES the set and by no other",
			applied)
	}
	if len(port.tokens) != 1 {
		t.Fatalf("the adapter was handed %d changes, want the one that was fully signed",
			len(port.tokens))
	}
	if asked := len(door.submitted) - submissionsAtRequest; asked != 1 {
		t.Fatalf("the door was asked %d times for one change, want once — the set is one gate, "+
			"not one gate per signature", asked)
	}

	recorded, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if recorded.State != ApprovalApproved {
		t.Fatalf("state = %q with every slot filled — %s", recorded.State,
			recorded.StillNeeded())
	}
	assertDistinctSignatories(t, recorded, required)

	// N+1 is impossible rather than unlikely: the record left the pending state on the
	// completing signature, so a late arrival finds nothing to sign.
	if _, err := manager.Approve(context.Background(),
		mantlekeep.Subject{ID: "approver-late"}, id); !errors.Is(err, ErrApprovalNotPending) {
		t.Fatalf("a signature was accepted after the set completed; got %v", err)
	}
	if len(port.tokens) != 1 {
		t.Fatal("the change was applied a second time")
	}
}

// A decline ends the request however many signatures it had collected, and the record keeps
// them — a change one person signed and another refused must read as both.
//
// There is deliberately no "N declines to block" counterpart: see NOTES.md.
func TestADeclineEndsAPartiallySignedSetAndKeepsTheSignatures(t *testing.T) {
	manager, _, store, port := signatureManager(t, 3)
	id := openGatedChange(t, manager)
	ctx := context.Background()

	if _, err := manager.Approve(ctx, mantlekeep.Subject{ID: "lead-bob"}, id); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	if err := manager.Decline(ctx, mantlekeep.Subject{ID: "arch-carol"}, id,
		"the rollback plan does not cover the schema change"); err != nil {
		t.Fatalf("declining a partially signed change: %v", err)
	}

	recorded, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if recorded.State != ApprovalDeclined {
		t.Fatalf("state = %q — one refusal ends the request, whatever the count was",
			recorded.State)
	}
	if got := recorded.Signatories(); len(got) != 1 || got[0] != "lead-bob" {
		t.Fatalf("signatories = %v — a declined record that dropped its signatures reads as "+
			"though nobody had ever agreed", got)
	}
	if _, err := manager.Approve(ctx, mantlekeep.Subject{ID: "sec-dave"}, id); !errors.Is(err,
		ErrApprovalNotPending) {
		t.Fatalf("a declined change kept collecting signatures; got %v", err)
	}
	if len(port.tokens) != 0 {
		t.Fatal("a declined change reached the adapter")
	}
}

// signAllAtOnce releases every approver at the same instant against the same change, and reports
// which of them the manager ACCEPTED and how many were told the change had been applied.
//
// Approvers who are refused are dropped rather than recorded: past the required number a refusal
// is the correct answer, so the interesting number is how many got in, not how many tried.
func signAllAtOnce(manager *Manager, id string, approvers int) (signed []string, applied int) {
	var (
		start = make(chan struct{})
		wait  sync.WaitGroup
		mu    sync.Mutex
	)
	for approver := 0; approver < approvers; approver++ {
		wait.Add(1)
		go func(n int) {
			defer wait.Done()
			<-start // released together, so the window is as narrow as the code allows
			who := fmt.Sprintf("approver-%d", n)
			result, err := manager.Approve(context.Background(),
				mantlekeep.Subject{ID: who}, id)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			signed = append(signed, who)
			if result.Applied() {
				applied++
			}
		}(approver)
	}
	close(start)
	wait.Wait()
	return signed, applied
}

// assertDistinctSignatories fails if the record holds a name twice, or holds a number of names
// other than the number of slots. Both are how a set silently stops being a set: one person
// filling two slots satisfies the count without satisfying the rule.
func assertDistinctSignatories(t *testing.T, recorded Approval, required int) {
	t.Helper()
	seen := map[string]bool{}
	for _, who := range recorded.Signatories() {
		if seen[who] {
			t.Fatalf("%q appears twice in %v — one person filled two slots under concurrency",
				who, recorded.Signatories())
		}
		seen[who] = true
	}
	if len(seen) != required {
		t.Fatalf("the record holds %d distinct signatures (%v), want %d", len(seen),
			recorded.Signatories(), required)
	}
}
