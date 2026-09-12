package estate

import (
	"context"
	"errors"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// This file drives a REQUIRED SET of signatures through the whole manager, against the door fake
// that behaves the way the real door does (see approval_flow_test.go). The floors on that set —
// the requester, a repeat signer, an AI — are in approval_signature_floors_test.go, because they
// are a different question: this file asks whether a set can be collected at all, that one asks
// what can never fill a slot.

const litSignaturesApplyV = "apply: %v"

// signatureManager is the loop of approval_flow_test.go with a floor that costs TWO distinct
// signatures at the platform gate, which is the whole difference under test.
func signatureManager(t *testing.T, required int) (*Manager, *realisticDoor, *MemoryApprovals,
	*recordingPort) {

	t.Helper()
	floor := DefaultFloor()
	floor.Revision = "floor-under-test"
	floor.Signatures = map[Gate]int{GatePlatform: required}
	door := &realisticDoor{}
	port := &recordingPort{asset: "kafka"}
	store := NewMemoryApprovals()
	manager := NewManager(door, floor, port).
		AwaitApprovalIn(store).
		FloorFrom(func() Floor { return floor })
	return manager, door, store, port
}

// openGatedChange asks for a change that the door refuses pending a person, and returns the
// approval a person can act on.
func openGatedChange(t *testing.T, manager *Manager) string {
	t.Helper()
	outcome, err := manager.Apply(context.Background(),
		mantlekeep.Subject{ID: "dev-alice"}, gatedManifest(t))
	if err != nil {
		t.Fatalf(litSignaturesApplyV, err)
	}
	if len(outcome.Refused) == 0 || outcome.Refused[0].Approval == "" {
		t.Fatalf("no approval was opened to act on: %+v", outcome)
	}
	return outcome.Refused[0].Approval
}

// The count comes from the FLOOR and is stamped on the record when the request is opened.
//
// Stored rather than re-read, for the same reason the resolved change and the floor revision
// are: a config edit must not turn a change two people have already signed into one that needs
// three, and nobody would have signed off on that requirement.
func TestTheRequiredCountIsResolvedFromTheFloorAndStored(t *testing.T) {
	manager, _, store, _ := signatureManager(t, 2)
	id := openGatedChange(t, manager)

	opened, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if opened.RequiredSignatures != 2 {
		t.Fatalf("requiredSignatures = %d, want the floor's 2 for the platform gate — a count "+
			"nobody wrote down is a count that changes when the document does",
			opened.RequiredSignatures)
	}
	if opened.SignaturesOutstanding() != 2 {
		t.Errorf("outstanding = %d on a freshly opened request", opened.SignaturesOutstanding())
	}
}

// Partial collection: one of two signatures leaves the change PENDING, visible, and saying who
// is still needed — and the door is not asked, because the change is not happening yet.
func TestOneOfTwoSignaturesLeavesTheChangeWaitingAndSaysWhoIsNeeded(t *testing.T) {
	manager, door, store, port := signatureManager(t, 2)
	id := openGatedChange(t, manager)
	submissionsAtRequest := len(door.submitted)

	result, err := manager.ApproveCiting(context.Background(),
		mantlekeep.Subject{ID: "lead-bob"}, id, "CHG-4471")
	if err != nil {
		t.Fatalf("the first of two signatures must be accepted, not refused: %v", err)
	}
	if result.Applied() {
		t.Fatal("a change with one of two signatures reported as applied — a partially signed " +
			"production change reported as done is the failure this whole feature exists to " +
			"prevent")
	}
	if result.Refused != "" {
		t.Errorf("refused = %q on a signature that was accepted — telling an approver who did "+
			"exactly what was asked that they were refused is how people stop using the queue",
			result.Refused)
	}
	for _, mustSay := range []string{"1 more signature", "lead-bob"} {
		if !strings.Contains(result.Outstanding, mustSay) {
			t.Errorf("outstanding = %q, which does not mention %q", result.Outstanding, mustSay)
		}
	}
	if result.Approval != id {
		t.Errorf("approval = %q, want %q — a partial result with nothing to poll is a dead end",
			result.Approval, id)
	}

	if len(port.tokens) != 0 {
		t.Fatal("the change reached the adapter on one of two signatures")
	}
	if len(door.submitted) != submissionsAtRequest {
		t.Error("the door was asked about a change whose signature set was incomplete — the " +
			"door decides whether a change may happen, and it is not happening yet")
	}

	// And a READER can see the partial state, whose signature it holds, and why they signed.
	partial := waitingInQueue(t, store, "payments", id)
	if got := partial.Signatories(); len(got) != 1 || got[0] != "lead-bob" {
		t.Fatalf("signatories = %v — silence here is what makes people ask in chat instead of "+
			"looking", got)
	}
	if partial.Signatures[0].Reference != "CHG-4471" {
		t.Errorf("the cited reference was lost (%q) — the chain can then say who signed but "+
			"not on what basis", partial.Signatures[0].Reference)
	}
}

// waitingInQueue returns the named change as an APPROVER would find it, and fails if the queue
// does not hold it. Read from the queue rather than by ID on purpose: a partially signed change
// that had left the queue would be invisible to the person who still has to finish it, which is
// a correct record nobody can act on.
func waitingInQueue(t *testing.T, store Approvals, team, id string) Approval {
	t.Helper()
	waiting, err := store.Pending(context.Background(), team)
	if err != nil {
		t.Fatalf("listing the queue: %v", err)
	}
	for _, candidate := range waiting {
		if candidate.ID == id {
			return candidate
		}
	}
	t.Fatalf("the partially signed change left the queue: %d waiting, none of them %s",
		len(waiting), id)
	return Approval{}
}

// Completion on the LAST signature: the change applies exactly once, submitted as the person
// who completed the set and naming the person who asked.
func TestTheLastSignatureCompletesTheSetAndAppliesTheChangeOnce(t *testing.T) {
	manager, door, store, port := signatureManager(t, 2)
	id := openGatedChange(t, manager)
	ctx := context.Background()

	if _, err := manager.Approve(ctx, mantlekeep.Subject{ID: "lead-bob"}, id); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	result, err := manager.Approve(ctx, mantlekeep.Subject{ID: "arch-carol"}, id)
	if err != nil {
		t.Fatalf("the completing signature: %v", err)
	}
	if !result.Applied() {
		t.Fatalf("the fully signed change did not reach the asset: refused=%q failed=%q "+
			"outstanding=%q", result.Refused, result.Failed, result.Outstanding)
	}
	if len(port.tokens) != 1 {
		t.Fatalf("the adapter was handed %d changes, want the one that was signed off",
			len(port.tokens))
	}

	// ONE submission for the whole set, made by the COMPLETING approver. This is exactly why
	// the distinctness floors cannot be left to the door: it only ever sees the last signature.
	approval := door.lastIntent(t)
	if approval.Subject.ID != "arch-carol" {
		t.Errorf("submitted as %q, want the completing approver", approval.Subject.ID)
	}
	if approval.Params["requester"] != "dev-alice" {
		t.Errorf("the approval carried requester %v, want the person who asked",
			approval.Params["requester"])
	}

	recorded, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if recorded.State != ApprovalApproved {
		t.Fatalf("state = %q with both signatures in", recorded.State)
	}
	// ApprovedBy is the COMPLETING signature — the one the change was applied on and the name
	// on the chain entry for the effect. The whole set is in Signatures.
	if recorded.ApprovedBy != "arch-carol" {
		t.Errorf("approvedBy = %q, want the completing signer", recorded.ApprovedBy)
	}
	if got := recorded.Signatories(); len(got) != 2 {
		t.Fatalf("signatories = %v, want both — ApprovedBy alone cannot be read as the whole "+
			"set, which is why the set is stored", got)
	}
	// A third person arriving late must find nothing left to sign.
	if _, err := manager.Approve(ctx, mantlekeep.Subject{ID: "sec-dave"}, id); !errors.Is(err,
		ErrApprovalNotPending) {
		t.Fatalf("a signature was accepted after the set completed; got %v", err)
	}
	if len(port.tokens) != 1 {
		t.Fatal("the change was applied twice")
	}
}

// A record written before signatures were a list must still approve on ONE signature.
//
// Old stored data is a test case, not an edge case. RequiredSignatures is zero on such a record,
// and a zero read literally would mean nobody need sign at all.
func TestAnOldSingleSignatureRecordStillApprovesOnOneSignature(t *testing.T) {
	manager, _, store, port := signatureManager(t, 2)
	ctx := context.Background()

	// Exactly the shape the previous version wrote: no RequiredSignatures, no Signatures. The
	// floor now says two, and this record must still mean what it meant when it was written.
	legacy := Approval{
		ID: "APR-payments-legacy", Team: "payments", State: ApprovalPending,
		Requester:     "dev-alice",
		RequiredRoles: []string{"L1-Architect"},
		FloorRevision: "floor-under-test",
		Change: DesiredItem{Asset: "kafka", Kind: "topic", Name: "payments.settlements",
			Tier: TierProd, Gate: GatePlatform},
		CreatedAt: manager.now().UTC(),
		ExpiresAt: manager.now().UTC().Add(24 * 7 * 3600 * 1e9),
	}
	if err := store.Open(ctx, legacy); err != nil {
		t.Fatalf("opening a historical record: %v", err)
	}

	result, err := manager.Approve(ctx, mantlekeep.Subject{ID: "arch-carol"}, legacy.ID)
	if err != nil {
		t.Fatalf("approving a record written before the count existed: %v", err)
	}
	if !result.Applied() {
		t.Fatalf("a historical single-signature approval did not apply: refused=%q "+
			"outstanding=%q — a zero count read literally turns every stored approval into "+
			"one that needs a signature nobody was ever asked for", result.Refused,
			result.Outstanding)
	}
	if len(port.tokens) != 1 {
		t.Fatalf("the adapter was handed %d changes", len(port.tokens))
	}
	recorded, _ := store.Get(ctx, legacy.ID)
	if recorded.ApprovedBy != "arch-carol" || len(recorded.Signatories()) != 1 {
		t.Errorf("record says approvedBy=%q signatories=%v", recorded.ApprovedBy,
			recorded.Signatories())
	}
}

// storeWithoutSigner is an [Approvals] written before a change could need several signatures: it
// has no atomic append. Declared here rather than mocked, because the point is a store that is
// CORRECT and simply predates the extension.
type storeWithoutSigner struct{ inner *MemoryApprovals }

func (s *storeWithoutSigner) Open(ctx context.Context, approval Approval) error {
	return s.inner.Open(ctx, approval)
}

func (s *storeWithoutSigner) Get(ctx context.Context, id string) (Approval, error) {
	return s.inner.Get(ctx, id)
}

func (s *storeWithoutSigner) Decide(ctx context.Context, approval Approval) error {
	return s.inner.Decide(ctx, approval)
}

func (s *storeWithoutSigner) Pending(ctx context.Context, team string) ([]Approval, error) {
	return s.inner.Pending(ctx, team)
}

// A store with no atomic append is REFUSED for a change needing several signatures, and still
// serves one needing a single signature.
//
// Collecting a partial signature without an atomic append loses one in a race: two approvers
// each read a record holding no signatures, each write a record holding their own, and a change
// two people signed waits forever for a third with nothing in the record to say why.
func TestAStoreThatCannotAppendAtomicallyIsRefusedForASetAndStillServesOne(t *testing.T) {
	floor := DefaultFloor()
	floor.Revision = "floor-under-test"
	floor.Signatures = map[Gate]int{GatePlatform: 2}
	port := &recordingPort{asset: "kafka"}
	store := &storeWithoutSigner{inner: NewMemoryApprovals()}
	manager := NewManager(&realisticDoor{}, floor, port).
		AwaitApprovalIn(store).
		FloorFrom(func() Floor { return floor })

	id := openGatedChange(t, manager)
	_, err := manager.Approve(context.Background(), mantlekeep.Subject{ID: "lead-bob"}, id)
	if !errors.Is(err, ErrStoreCannotCollectSignatures) {
		t.Fatalf("a store with no atomic append accepted one of two signatures; got %v — the "+
			"next concurrent signer overwrites this one and the change never applies", err)
	}
	if !strings.Contains(err.Error(), "estate.Signer") {
		t.Errorf("the error does not say what to implement: %v", err)
	}

	// And the single-signature case is untouched: the published Decide is the right guard when
	// the signature that arrives is also the one that completes the set.
	floor.Signatures = map[Gate]int{GatePlatform: 1}
	single := openGatedChange(t, manager)
	result, err := manager.Approve(context.Background(), mantlekeep.Subject{ID: "lead-bob"},
		single)
	if err != nil || !result.Applied() {
		t.Fatalf("a store that predates the extension must still serve a one-signature "+
			"change: err=%v result=%+v", err, result)
	}
}

// Config may require MORE signatures and can never require fewer. The seam, again: config
// chooses the policy, it cannot reach the guarantee.
func TestTheFloorCanOnlyRaiseTheRequiredSignatureCount(t *testing.T) {
	floor := DefaultFloor()
	floor.Signatures = map[Gate]int{GatePlatform: 0, GateOwningTeam: 3}

	if got := floor.SignaturesFor(GatePlatform); got != 1 {
		t.Errorf("config asked for %d signatures at the platform gate and got %d — a document "+
			"that can lower this can un-gate production while every validation passes", 0, got)
	}
	if got := floor.SignaturesFor(GateOwningTeam); got != 3 {
		t.Errorf("config raised the owning-team gate to 3 and got %d — raising is the whole "+
			"point of putting the count in config", got)
	}
	// An unrecognised gate is not assumed permissive, the same reading Gate.Strength gives it.
	if got := floor.SignaturesFor(Gate("invented-by-a-typo")); got != 1 {
		t.Errorf("an unknown gate resolved to %d signatures, want 1 — a typo in a document "+
			"must not be the thing that removes a signature", got)
	}
}
