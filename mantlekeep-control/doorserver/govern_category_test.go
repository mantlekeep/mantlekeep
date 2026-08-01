package doorserver

import (
	"testing"

	mantlekeep "mantlekeep.dev/control"
)

// The wire code for a denial must come from the category the ENGINE stamped, not from
// guessing at the human reason. These tests pin that precedence, because guessing had
// already misclassified the most important denial in the system.

func TestStampedCategoryDecidesTheWireCode(t *testing.T) {
	cases := []struct {
		name     string
		category mantlekeep.DenialCategory
		want     string
	}{
		{"floor", mantlekeep.DenialFloor, codeFloor},
		{"separation of duties", mantlekeep.DenialSeparationOfDuties, codeSeparationDuties},
		{"identity", mantlekeep.DenialIdentity, codeIdentity},
		{"action not allowed", mantlekeep.DenialActionNotAllowed, codeActionNotAllowed},
		{"validation", mantlekeep.DenialValidation, codeValidation},
		{"policy error", mantlekeep.DenialPolicyError, codePolicyError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wireCodeFor(mantlekeep.Decision{Action: mantlekeep.ActionDeny, Category: tc.category})
			if got != tc.want {
				t.Errorf("category %q should map to %q, got %q", tc.category, tc.want, got)
			}
		})
	}
}

// The sealed floor is the regression that motivated this: the reason "AI agents cannot
// approve" matches no separation-of-duties substring, so the reason-guessing fallback
// classifies it as DENY_POLICY_ERROR — the least specific code, for the most important
// refusal. With the engine stamping the category, the reason text no longer decides.
func TestSealedFloorDenialIsNotMisclassified(t *testing.T) {
	sealed := mantlekeep.Decision{
		Action:   mantlekeep.ActionDeny,
		Reason:   "AI agents cannot approve: sdlc.release", // wording the old matcher missed
		Category: mantlekeep.DenialSeparationOfDuties,
	}
	if got := wireCodeFor(sealed); got != codeSeparationDuties {
		t.Fatalf("the sealed-floor denial must be %q regardless of reason wording, got %q",
			codeSeparationDuties, got)
	}
	// Prove the guess alone would have failed — this is what the stamp protects against.
	if guessed := denialCode(sealed.Reason); guessed == codeSeparationDuties {
		t.Skip("reason-guessing happens to match; the stamp is what guarantees it does not have to")
	}
}

// A Decision from an EXTERNAL evaluator (OPA/Cedar) carries no category. The wire must
// still land it on a sensible code by classifying the reason — the fallback path.
func TestUncategorisedDecisionFallsBackToReason(t *testing.T) {
	bare := mantlekeep.Decision{
		Action: mantlekeep.ActionDeny,
		Reason: "no role permits action foo.bar",
		// Category deliberately unset: an external engine that returns a plain Decision.
	}
	if got := wireCodeFor(bare); got != codeActionNotAllowed {
		t.Errorf("an uncategorised deny should fall back to reason classification (%q), got %q",
			codeActionNotAllowed, got)
	}
}
