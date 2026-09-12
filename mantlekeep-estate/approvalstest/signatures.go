// This file is one concern: the conformance cases that prove a required SET of signatures can be
// COLLECTED, completed and read — including on a record written before signatures were a list.
//
// Split from conformance.go because it proves a different contract. conformance.go proves the
// [estate.Approvals] rule — one DECISION per record, whatever reached it. These prove the
// [estate.Signer] rule — an append that accumulates a set and completes it on the last signature.
// What can never fill a slot, and the count under concurrency, are in signature_floors.go.
package approvalstest

import (
	"context"
	"strings"
	"testing"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// needing is a pending approval requiring a SET of `signatures` distinct people, naming a role so
// the "who is still needed" message has something true to say.
// Named because the same sentence is asserted in several cases below.
const litReadingBack = "reading back: %v"

func needing(id, team string, signatures int) estate.Approval {
	waiting := pending(id, team)
	waiting.RequiredSignatures = signatures
	waiting.RequiredRoles = []string{"L1-Architect", "L2-Lead"}
	return waiting
}

func signedBy(approver string) estate.Signature {
	return estate.Signature{By: approver, At: time.Now().UTC(), Reference: "CHG-" + approver}
}

// signerOrSkip returns the store's atomic append, or skips the case with the reason.
//
// A skip and not a failure. A store predating [estate.Signer] is not broken — it can serve every
// change needing ONE signature, and [estate.Manager.Approve] refuses a change needing more against
// it rather than losing a signature in a race. Saying that out loud in the test output is the
// point: an operator reading -race output learns the limit of the store they configured instead of
// discovering it from a change that never applies.
func signerOrSkip(t *testing.T, store estate.Approvals) estate.Signer {
	t.Helper()
	signer, atomic := store.(estate.Signer)
	if !atomic {
		t.Skipf("%T does not implement estate.Signer, so it cannot collect more than one "+
			"signature atomically and the manager will refuse a change that requires more. "+
			"Implement Sign — appending under the same lock that checks distinctness — to "+
			"serve changes requiring a set of signatures.", store)
	}
	return signer
}

func readBack(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Open(ctx, pending("AP-1", "payments")); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	found, err := store.Get(ctx, "AP-1")
	if err != nil {
		t.Fatalf(litReadingBack, err)
	}
	if found.State != estate.ApprovalPending {
		t.Fatalf("a freshly opened approval is pending, got %q", found.State)
	}
}

// A change requiring two signatures with one collected is PENDING, and a reader can see it has
// one and whose.
//
// Silence here is what makes people ask in chat instead of looking, and the thing they try next
// is the path that does not ask.
func partialSetStaysPending(t *testing.T, newStore Factory) {
	store := newStore(t)
	signer := signerOrSkip(t, store)
	ctx := context.Background()

	if err := store.Open(ctx, needing("AP-PARTIAL", "payments", 2)); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	after, err := signer.Sign(ctx, "AP-PARTIAL", signedBy("lead-bob"))
	if err != nil {
		t.Fatalf("the first of two signatures must be accepted: %v", err)
	}
	if after.State != estate.ApprovalPending {
		t.Fatalf("state = %q after one of two signatures — a change whose set is incomplete "+
			"must still be waiting", after.State)
	}
	if after.ApprovedBy != "" {
		t.Errorf("approvedBy = %q with a signature still outstanding — that field is the "+
			"COMPLETING signature, and setting it early names somebody as having released a "+
			"change that has not been released", after.ApprovedBy)
	}

	// Read back through the STORE, not from the value Sign returned: a reader looking at the
	// queue must see the partial state, or nobody but the signer knows it exists.
	found, err := store.Get(ctx, "AP-PARTIAL")
	if err != nil {
		t.Fatalf(litReadingBack, err)
	}
	if got := found.Signatories(); len(got) != 1 || got[0] != "lead-bob" {
		t.Fatalf("signatories = %v, want the one person who signed — a partial set nobody can "+
			"read is indistinguishable from no progress at all", got)
	}
	if found.SignaturesOutstanding() != 1 {
		t.Errorf("outstanding = %d, want 1", found.SignaturesOutstanding())
	}
	// The words a person acts on. It must name how many more AND who may give them.
	needed := found.StillNeeded()
	for _, mustSay := range []string{"1 more signature", "L1-Architect", "lead-bob"} {
		if !strings.Contains(needed, mustSay) {
			t.Errorf("StillNeeded() = %q, which does not mention %q — a wait that cannot say "+
				"who unblocks it is a dead end wearing the shape of a process", needed, mustSay)
		}
	}
	// And it stays in the queue, because it is still somebody's work.
	waiting, err := store.Pending(ctx, "payments")
	if err != nil || len(waiting) != 1 {
		t.Fatalf("a partially signed change must stay in the queue: %d waiting, err=%v",
			len(waiting), err)
	}
}

// The set completes on the LAST signature, and that signature is the one the record names.
func lastSignatureDecides(t *testing.T, newStore Factory) {
	store := newStore(t)
	signer := signerOrSkip(t, store)
	ctx := context.Background()

	if err := store.Open(ctx, needing("AP-COMPLETE", "payments", 3)); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	for _, approver := range []string{"lead-bob", "arch-carol"} {
		after, err := signer.Sign(ctx, "AP-COMPLETE", signedBy(approver))
		if err != nil {
			t.Fatalf("signature from %s: %v", approver, err)
		}
		if after.State != estate.ApprovalPending {
			t.Fatalf("the set completed at %d of 3 signatures", len(after.Signatories()))
		}
	}

	decided, err := signer.Sign(ctx, "AP-COMPLETE", signedBy("sec-dave"))
	if err != nil {
		t.Fatalf("the completing signature: %v", err)
	}
	if decided.State != estate.ApprovalApproved {
		t.Fatalf("state = %q once every required signature is in — the set is complete and "+
			"nothing else is going to arrive", decided.State)
	}
	// ApprovedBy is the COMPLETING signature: the one the change is applied on, the one the
	// door is submitted as, and the name on the chain entry for the effect.
	if decided.ApprovedBy != "sec-dave" {
		t.Errorf("approvedBy = %q, want the completing signer", decided.ApprovedBy)
	}
	if decided.DecidedAt.IsZero() {
		t.Error("decidedAt is zero on a completed set — the record cannot say when it was released")
	}
	if got := decided.Signatories(); len(got) != 3 {
		t.Fatalf("signatories = %v, want all three — the whole set is the record of who agreed", got)
	}
	// Every signature keeps its own reference, so the chain can cite why each person signed
	// rather than only that somebody did.
	for _, signature := range decided.Signatures {
		if signature.Reference == "" {
			t.Errorf("the signature from %q lost its reference — a chain entry naming a person "+
				"and nothing else says somebody agreed, not what they agreed to", signature.By)
		}
		if signature.At.IsZero() {
			t.Errorf("the signature from %q has no time", signature.By)
		}
	}
	// And it leaves the queue, or approvers keep seeing work that is done.
	if waiting, _ := store.Pending(ctx, "payments"); len(waiting) != 0 {
		t.Errorf("a completed change stayed in the queue: %d waiting", len(waiting))
	}
}

// An approval written BEFORE signatures were a list must still read back as approved.
//
// Old stored data is a test case, not an edge case. Such a record carries its single signer in
// ApprovedBy, holds no signature list, and has RequiredSignatures zero — and every one of those
// would be read wrongly by arithmetic that trusted the new fields literally: zero required would
// mean nobody need sign, and an empty list would mean nobody had.
func oldSingleSignatureRecordReadsBack(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	// Exactly the shape the previous version wrote: no RequiredSignatures, no Signatures.
	legacy := pending("AP-LEGACY", "payments")
	legacy.State = estate.ApprovalApproved
	legacy.ApprovedBy = "lead-bob"
	legacy.DecidedAt = time.Now().UTC()
	if err := store.Open(ctx, legacy); err != nil {
		t.Fatalf(wrapOpening, err)
	}

	found, err := store.Get(ctx, "AP-LEGACY")
	if err != nil {
		t.Fatalf(litReadingBack, err)
	}
	if found.SignaturesRequired() != 1 {
		t.Errorf("required = %d on a record written before the field existed — zero read "+
			"literally turns every historical approval into one that needed nobody",
			found.SignaturesRequired())
	}
	if got := found.Signatories(); len(got) != 1 || got[0] != "lead-bob" {
		t.Fatalf("signatories = %v, want the person in ApprovedBy — counting only the list "+
			"makes every approval ever granted read back as unsigned", got)
	}
	if found.SignaturesOutstanding() != 0 {
		t.Errorf("outstanding = %d on a record that was approved", found.SignaturesOutstanding())
	}
	if !found.HasSignatureFrom("lead-bob") {
		t.Error("the historical signer does not register as having signed, so they could be " +
			"asked to sign again and the set would then hold one person twice")
	}
	if needed := found.StillNeeded(); !strings.Contains(needed, "nothing") {
		t.Errorf("StillNeeded() = %q on a fully approved record", needed)
	}
}
