package estate

import (
	"context"
	"fmt"
)

// This file is one concern: adding ONE signature to a pending approval, against whichever store
// a deployment configured.
//
// It exists because [Approvals] is a published port and [Signer] had to be added beside it
// rather than into it. That leaves exactly two cases, and the point of writing them once here
// is that [Manager.Approve] reads as one linear path instead of branching on a type assertion
// in the middle of the rules it is enforcing.

// collectSignature appends one signature and returns the record as it now stands.
//
// A store implementing [Signer] does the whole thing under its own lock, which is the only way
// it can be race-free. A store that does not is not broken — it is a store written before a
// change could need more than one signature — and it can still serve the single-signature case
// through the published Decide, because there the signature that arrives is also the signature
// that completes the set, and Decide's compare-and-set is exactly the right guard for that.
//
// What it must never do is collect a PARTIAL signature without an atomic append. Two approvers
// would each read a record holding no signatures, each write a record holding their own, and
// one signature would vanish: a change two people signed would sit waiting for a third, and
// nothing in the record would say why. So that case is refused, naming the store, and the
// deployment gets an error it can act on instead of an approval queue that loses work.
func collectSignature(ctx context.Context, store Approvals, approval Approval,
	signature Signature) (Approval, error) {

	if signer, atomic := store.(Signer); atomic {
		return signer.Sign(ctx, approval.ID, signature)
	}
	if approval.SignaturesRequired() > 1 {
		return Approval{}, fmt.Errorf("%w (%T was asked for %d distinct signatures on %s)",
			ErrStoreCannotCollectSignatures, store, approval.SignaturesRequired(), approval.ID)
	}
	// The pre-[Signer] path, unchanged in behaviour: one signature completes the set, so the
	// record is decided here and Decide refuses to overwrite one already decided.
	approval.Signatures = []Signature{signature}
	approval.State = ApprovalApproved
	approval.ApprovedBy = signature.By
	approval.DecidedAt = signature.At
	if err := store.Decide(ctx, approval); err != nil {
		return Approval{}, err
	}
	return approval, nil
}
