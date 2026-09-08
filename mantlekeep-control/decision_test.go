package mantlekeep

import (
	"errors"
	"fmt"
	"testing"
)

// The bug this prevents, stated as a test: a caller that reads only one error shape reports a
// change WAITING FOR A PERSON as forbidden. It happened, was fixed, and happened again in a
// second handler when the door's error type changed.

func TestTheDoorsDecisionIsRecoverableFromEitherErrorShape(t *testing.T) {
	waiting := Decision{
		Action:            ActionRequireApproval,
		Reason:            "a second person must sign off",
		RequiredApprovers: []Role{RoleOperator},
	}
	for name, err := range map[string]error{
		"the shape the door returns": &DecisionError{Decision: waiting},
		"the narrower older shape": &Refused{
			Action:            waiting.Action,
			Reason:            waiting.Reason,
			RequiredApprovers: waiting.RequiredApprovers,
		},
		"wrapped, as a caller would find it": fmt.Errorf("governing the change: %w",
			&DecisionError{Decision: waiting}),
	} {
		t.Run(name, func(t *testing.T) {
			decision, ok := DecisionFrom(err)
			if !ok {
				t.Fatal("the door's decision was not recovered — the caller falls through to its " +
					"default, which is how a pending approval gets reported as a denial")
			}
			if decision.Action != ActionRequireApproval {
				t.Fatalf("the decision reads %q, want require_approval", decision.Action)
			}
			if len(decision.RequiredApprovers) != 1 {
				t.Fatal("who may sign off was lost — a refusal that cannot name an approver is a " +
					"dead end wearing the shape of a process")
			}
			if !AwaitingApproval(err) {
				t.Fatal("AwaitingApproval says this is final; it is a request with a name on it")
			}
		})
	}
}

// A transport failure is not a policy decision. Reporting one as the other tells a person their
// request was refused when in fact nobody was asked.
func TestSomethingThatIsNotADecisionIsNotReportedAsOne(t *testing.T) {
	for name, err := range map[string]error{
		"a transport failure": errors.New("dial tcp: connection refused"),
		"no error at all":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			if decision, ok := DecisionFrom(err); ok {
				t.Fatalf("%q was read as the door deciding %q", name, decision.Action)
			}
			if AwaitingApproval(err) {
				t.Fatalf("%q was read as waiting for an approver", name)
			}
		})
	}
}

// A denial and a pending approval must not be confused in the other direction either: chasing an
// approver for something that will never be allowed wastes the one thing a gate costs — time.
func TestADenialIsNotReportedAsAwaitingApproval(t *testing.T) {
	denied := &DecisionError{Decision: Decision{
		Action: ActionDeny, Reason: "no role permits action estate.apply",
	}}
	decision, ok := DecisionFrom(denied)
	if !ok || decision.Action != ActionDeny {
		t.Fatalf("a denial recovered as ok=%v action=%q", ok, decision.Action)
	}
	if AwaitingApproval(denied) {
		t.Fatal("a final denial was reported as waiting for a person — somebody would go looking " +
			"for an approver who cannot help them")
	}
}
