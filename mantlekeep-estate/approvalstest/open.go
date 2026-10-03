package approvalstest

import (
	"context"
	"errors"
	"testing"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// Open must refuse an id the store already holds.
//
// A store that overwrites on Open fails silently and totally: a DECIDED approval becomes pending
// again under the id somebody already signed, with whoever re-opened it recorded as the requester.
// The next approval of that id is then refused as a self-approval — the two-party rule firing
// against the person who did nothing wrong — and the record of who approved is gone. Ids can
// collide for reasons no single caller controls: a store shared by several replicas, a replayed
// request, a caller supplying its own id.
func openRefusesATakenID(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Open(ctx, pending("AP-TAKEN", "payments")); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	if err := store.Open(ctx, pending("AP-TAKEN", "payments")); !errors.Is(err,
		estate.ErrApprovalExists) {
		t.Fatalf("a taken id was opened a second time; err = %v, want estate.ErrApprovalExists", err)
	}
}

func openKeepsADecidedApproval(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Open(ctx, pending(approvalDecided, "payments")); err != nil {
		t.Fatalf(wrapOpening, err)
	}
	approved := pending(approvalDecided, "payments")
	approved.State = estate.ApprovalApproved
	approved.ApprovedBy = approverBob
	if err := store.Decide(ctx, approved); err != nil {
		t.Fatalf("deciding: %v", err)
	}

	// The same id opened again, as the approver — what a colliding id produces.
	reopened := pending(approvalDecided, "payments")
	reopened.Requester = approverBob
	_ = store.Open(ctx, reopened) // refused or not, the decided record must survive

	after, err := store.Get(ctx, approvalDecided)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if after.State != estate.ApprovalApproved || after.ApprovedBy != approverBob ||
		after.Requester != "dev-alice" {
		t.Fatalf("a decided approval was overwritten by Open: state=%q approvedBy=%q requester=%q",
			after.State, after.ApprovedBy, after.Requester)
	}
}
