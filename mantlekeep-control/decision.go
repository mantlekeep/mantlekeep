package mantlekeep

import "errors"

// This file answers one question a caller cannot avoid: the door said no — what KIND of no?

// DecisionFrom recovers the door's decision from an error, whatever shape it arrived in.
//
// # Why this exists
//
// Submit reports a non-allow outcome as an error, and there are two error shapes that carry a
// decision: [DecisionError], which the door returns and which carries the whole [Decision], and
// [Refused], the narrower shape some callers still construct. A caller that checks only one of
// them does not get a compile error when it meets the other — it gets a type assertion that
// silently misses, falls through to its default, and reports a change WAITING FOR A PERSON as
// forbidden.
//
// That is not hypothetical. It happened, was fixed, and then happened again in a second handler
// when the door's error type changed: a gated estate change was answered "deny", so nothing ever
// opened an approval and the whole approval path was unreachable in a running system while every
// unit test passed. Every caller writing its own type switch is what made one change able to
// break it twice.
//
// The second return distinguishes "the door decided this" from "something else went wrong".
// Reporting a transport failure as a policy denial tells a person their request was refused when
// in fact nobody was asked.
func DecisionFrom(err error) (Decision, bool) {
	if err == nil {
		return Decision{}, false
	}
	var decisionErr *DecisionError
	if errors.As(err, &decisionErr) {
		return decisionErr.Decision, true
	}
	var refused *Refused
	if errors.As(err, &refused) {
		return Decision{
			Action:            refused.Action,
			Reason:            refused.Reason,
			RequiredApprovers: refused.RequiredApprovers,
		}, true
	}
	return Decision{}, false
}

// AwaitingApproval reports whether err is the door saying a PERSON is needed.
//
// Distinct from a denial in the way that matters most to whoever is waiting: a denial is final
// and a pending approval is a request with a name attached to it. A caller that cannot tell them
// apart either chases an approver for something that will never be allowed, or gives up on
// something that only needed a signature.
func AwaitingApproval(err error) bool {
	decision, ok := DecisionFrom(err)
	return ok && decision.Action == ActionRequireApproval
}
