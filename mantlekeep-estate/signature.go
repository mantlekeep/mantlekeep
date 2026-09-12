package estate

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// This file is one concern: what a SIGNATURE is, and what a SET of them means.
//
// It exists because an approval stopped being one person's decision. A change can require N
// DISTINCT signatures, and everything that follows from that — how many are still outstanding,
// who has already signed, who may still sign, and how an old record written when there was only
// ever one signer reads back — is set arithmetic over a stored record rather than anything to do
// with the door, the store, or the manager. Keeping it here is what stops that arithmetic being
// re-derived, slightly differently, at each of the three places that need it.
//
// A SET, never a SEQUENCE. There is deliberately no ordering, no stage and no "whose turn it
// is": see NOTES.md. An ordered process is two separately gated actions, not one gate with
// steps, and every predecessor of this system that grew stages was worked around.

// Signature is ONE person's sign-off on a pending change.
//
// Who, when, and optionally WHY. The reference is a ticket or record id the approver cites, and
// it exists so the chain can answer "on what basis?" rather than only "who?" — a hash-chain
// entry naming a person and nothing else tells an auditor that somebody agreed, not what they
// were agreeing to. Optional because an approver acting on a conversation is still accountable,
// and demanding a ticket number that does not exist yet teaches people to type anything.
//
// A value type: it is copied, compared and stored, never mutated in place.
type Signature struct {
	// By is the approver's resolved subject id — the door's name for them, not one they typed.
	By string `json:"by"`
	// At is when they signed, in UTC. Stored per signature rather than derived from the
	// record's DecidedAt, because with several signatures there is no single moment a change
	// was approved: there is the moment each person signed, and the last of them completes it.
	At time.Time `json:"at"`
	// Reference is the ticket or record the approver cites, when they cite one.
	Reference string `json:"reference,omitempty"`
}

// Errors a caller must be able to tell apart, because each sends a person somewhere different.
var (
	// ErrDuplicateSignature — one person tried to fill two slots. THE floor of a required SET:
	// N signatures from one person is one signature, and separation of duties is not a policy
	// this deployment can relax.
	ErrDuplicateSignature = errors.New(
		"estate: this approver has already signed this change — the required signatures must " +
			"come from DISTINCT people, and one person signing twice is still one person; " +
			"ask somebody holding one of the roles this change still needs")
	// ErrAIApproval — an automation tried to fill a slot. Also a floor, and the one that makes
	// a required SET safe: with several signatures the door is asked ONCE, by whoever completes
	// the set, so every signature before the last is never seen by the core's "an AI may never
	// approve" rule. If this were not checked here, requiring two signatures would be a way to
	// get an AI's name onto an approval that the core refuses outright.
	ErrAIApproval = errors.New(
		"estate: an AI agent may not sign an approval — a required set of signatures is a set " +
			"of PEOPLE, and an automation filling one slot leaves the change approved by " +
			"fewer humans than the floor demands; approve as the person accountable for it")
	// ErrSignaturesOutstanding — something tried to mark a record approved while its signature
	// set was incomplete. Refused by the STORE, so the requirement cannot be reduced to
	// decoration by one caller writing the state it wanted.
	ErrSignaturesOutstanding = errors.New(
		"estate: this change cannot be marked approved while signatures are outstanding — " +
			"collect the remaining ones through Manager.Approve, which appends a signature " +
			"at a time and decides the record only when the set is complete")
	// ErrStoreCannotCollectSignatures — the configured store predates [Signer] and therefore
	// has no atomic append. It can serve a change needing ONE signature exactly as it always
	// did; it cannot serve a change needing several without losing one in a race, so it is
	// refused rather than quietly under-collecting.
	ErrStoreCannotCollectSignatures = errors.New(
		"estate: the configured approval store cannot collect more than one signature " +
			"atomically — implement estate.Signer on it, or configure a store that does, " +
			"before requiring more than one signature in the floor")
)

// SignaturesRequired is how many DISTINCT people must sign before this change is applied.
//
// Zero reads as ONE, and that is the whole backward-compatibility story for the stored record:
// every approval written before this field existed meant "one person must sign", and a zero
// treated literally would mean "nobody need sign" — an old record turning into an ungated one
// the moment new code read it.
func (a Approval) SignaturesRequired() int {
	if a.RequiredSignatures < 1 {
		return 1
	}
	return a.RequiredSignatures
}

// CollectedSignatures is the sign-offs gathered so far, reading an OLD record correctly.
//
// A record written before signatures were a list carries its single signer in ApprovedBy alone.
// Counting only the list would make every approval ever granted read back as unsigned — and the
// store's "you may not approve with signatures outstanding" rule would then refuse to record a
// decision on a record that already had one. Old stored data is a test case, not an edge case.
func (a Approval) CollectedSignatures() []Signature {
	if len(a.Signatures) > 0 {
		return cloneSignatures(a.Signatures)
	}
	if a.ApprovedBy != "" {
		return []Signature{{By: a.ApprovedBy, At: a.DecidedAt}}
	}
	return nil
}

// SignaturesOutstanding is how many more DISTINCT people must sign. Never negative: a record
// that somehow holds more signatures than it requires needs nobody else, and reporting "-1
// signatures needed" would put that arithmetic in front of a person instead of an answer.
func (a Approval) SignaturesOutstanding() int {
	outstanding := a.SignaturesRequired() - len(a.CollectedSignatures())
	if outstanding < 0 {
		return 0
	}
	return outstanding
}

// HasSignatureFrom reports whether this person has already signed — the check that keeps the
// set DISTINCT. Compared on the resolved subject id, which is the only name the approver did
// not choose for themselves.
func (a Approval) HasSignatureFrom(approver string) bool {
	for _, signature := range a.CollectedSignatures() {
		if signature.By == approver {
			return true
		}
	}
	return false
}

// Signatories names everyone who has signed, in the order they signed.
//
// Order of ARRIVAL, which is a fact, as distinct from an order of PRECEDENCE, which this
// feature does not have. Nothing reads this to decide who may sign next.
func (a Approval) Signatories() []string {
	collected := a.CollectedSignatures()
	names := make([]string, 0, len(collected))
	for _, signature := range collected {
		names = append(names, signature.By)
	}
	return names
}

// StillNeeded is one sentence a person can act on: how many more signatures, who may give them,
// and who has already signed.
//
// This is why the record carries a signature list at all. A change needing two signatures with
// one collected is PENDING, and "pending" on its own is what makes somebody ask in chat instead
// of looking — a refusal that cannot say who unblocks it is a dead end wearing the shape of a
// process. So the wait has words, and they name the remaining roles rather than the people,
// because which people hold a role is the directory's answer and never this record's.
func (a Approval) StillNeeded() string {
	outstanding := a.SignaturesOutstanding()
	if outstanding == 0 {
		return "nothing: every required signature has been collected"
	}
	needed := fmt.Sprintf("%d more signature", outstanding)
	if outstanding > 1 {
		needed += "s"
	}
	switch {
	case len(a.RequiredRoles) > 0:
		needed += " from somebody holding one of: " + strings.Join(a.RequiredRoles, ", ")
	default:
		// No role on the record is not the same as "anybody": the door still resolves whether
		// the approver holds the authority. Say that, rather than implying a free-for-all.
		needed += " from a second party the door will accept (this record names no role, so " +
			"the door decides from the directory)"
	}
	if signed := a.Signatories(); len(signed) > 0 {
		needed += "; already signed by " + strings.Join(signed, ", ")
	}
	return needed
}

// cloneSignatures copies a signature list.
//
// Not defensiveness for its own sake. A store hands out a COPY of the record struct, but a
// slice field in that copy still points at the store's own backing array: a caller that
// appended to it would write into the stored record, outside the store's lock, and under -race
// that is a data race with every other reader. Copying on the way in and on the way out is what
// makes "the store owns its records" true rather than merely intended.
func cloneSignatures(signatures []Signature) []Signature {
	if len(signatures) == 0 {
		return nil
	}
	copied := make([]Signature, len(signatures))
	copy(copied, signatures)
	return copied
}
