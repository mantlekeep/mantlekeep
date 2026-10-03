package approvalstest

// The people and approval ids the cases share.
//
// Named once so two cases that mean "the same approver" cannot drift to different spellings and
// both still pass: a case that signs as "lead-bob" and checks a refusal for "lead-bobb" is testing
// a person nobody asked to sign.
const (
	// approverBob is the second, distinct signer most cases need: not the requester, and not
	// the first approver.
	approverBob = "lead-bob"

	approvalTwice    = "AP-TWICE"
	approvalSelf     = "AP-SELF"
	approvalSetRace  = "AP-SET-RACE"
	approvalShortcut = "AP-SHORTCUT"
	approvalPartial  = "AP-PARTIAL"
	approvalComplete = "AP-COMPLETE"
)
