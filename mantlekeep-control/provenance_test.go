package mantlekeep_test

import (
	"strings"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

func at(seconds int) time.Time {
	return time.Unix(int64(seconds), 0).UTC()
}

// An anonymous report cannot be attributed, corrected, or disbelieved. Accepting one would make
// every other reported record less trustworthy by association.
func TestAReportMustNameItsSource(t *testing.T) {
	if _, err := mantlekeep.ReportedBy("", at(1)); err == nil {
		t.Fatal("an anonymous report was accepted — nobody could be held to it")
	} else if !strings.Contains(err.Error(), "must name the principal") {
		t.Fatalf("the refusal must say why; got %v", err)
	}

	reported, err := mantlekeep.ReportedBy("raas", at(1))
	if err != nil {
		t.Fatalf("a named report must be accepted: %v", err)
	}
	if reported.Source != "raas" || reported.Verified() {
		t.Fatalf("testimony must carry its source and not claim to be verified; got %+v", reported)
	}
}

// The distinction is the whole point: a fact we checked and a fact we were told are different
// evidence, and a record that flattens them lets testimony be read as proof.
func TestFirsthandAndReportedAreNotTheSameThing(t *testing.T) {
	if !mantlekeep.Observed(at(1)).Verified() {
		t.Fatal("a firsthand observation must report itself as verified")
	}
	reported, _ := mantlekeep.ReportedBy("worker-7", at(1))
	if reported.Verified() {
		t.Fatal("testimony claimed to be verified — that is exactly the flattening this prevents")
	}
	if mantlekeep.Observed(at(1)).Source != "" {
		t.Fatal("a firsthand observation needs no attribution beyond the platform itself")
	}
}

// A platform that prefers testimony when it is easier has stopped being a record of what
// happened and become a record of what it was told.
func TestFirsthandAlwaysWinsEvenWhenOlder(t *testing.T) {
	seen := mantlekeep.Observed(at(1))                // older
	told, _ := mantlekeep.ReportedBy("raas", at(999)) // newer, but testimony

	if got := mantlekeep.Prefer(seen, told); !got.Verified() {
		t.Fatal("newer testimony beat older evidence — convenience decided the record")
	}
	if got := mantlekeep.Prefer(told, seen); !got.Verified() {
		t.Fatal("argument order changed the answer; evidence must win either way")
	}
}

// Between two of the same kind, the later one is the current one.
func TestBetweenTwoOfAKindTheLaterOneWins(t *testing.T) {
	early, _ := mantlekeep.ReportedBy("raas", at(1))
	late, _ := mantlekeep.ReportedBy("raas", at(2))

	if got := mantlekeep.Prefer(early, late); !got.At.Equal(at(2)) {
		t.Fatalf("the later report is the current one; got %v", got.At)
	}
	if got := mantlekeep.Prefer(mantlekeep.Observed(at(1)), mantlekeep.Observed(at(2))); !got.At.Equal(at(2)) {
		t.Fatalf("the later observation is the current one; got %v", got.At)
	}
}

// When a principal's report does not match what the platform read, at least one of them is
// wrong — and which is a question for a human, not a tiebreak for a function.
func TestADisagreementIsAFindingNotSomethingToResolve(t *testing.T) {
	seen := mantlekeep.Observed(at(2))
	told, _ := mantlekeep.ReportedBy("raas", at(1))

	if !mantlekeep.Disagree(seen, told, false) {
		t.Fatal("evidence contradicting testimony must surface — silently preferring one " +
			"discards the only signal that the reporter's records are wrong")
	}
	if mantlekeep.Disagree(seen, told, true) {
		t.Fatal("agreement is not a finding")
	}
	// Two reports disagreeing is not this: neither was verified, so there is nothing to
	// contradict. It is two rumours, and the caller decides what that is worth.
	otherTold, _ := mantlekeep.ReportedBy("worker-7", at(2))
	if mantlekeep.Disagree(otherTold, told, false) {
		t.Fatal("two unverified reports were called a contradiction of evidence")
	}
}
