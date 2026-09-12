package governancetest

// The shared probe: one submission through the real door, and a reading of what it said.
//
// Every behavioural check below is the engine's own answer rather than the suite's opinion of
// the documents, which is what makes those checks see grants that arrive from a registered
// policy provider or a config layer and never appear in a document at all.
//
// Submitting is SAFE to do repeatedly. Submit decides, records, and returns a token; it executes
// nothing (internal/sdk/sdk.go is explicit that withholding the capability, not the call, is
// what prevents an effect). A probe that is allowed therefore costs one audit record.

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// probeCount makes every probe's intent id unique. A chain of records that share an id can
// still be verified but can no longer be read back to a request, and this suite writes many.
var probeCount atomic.Uint64

// verdict is what the door said about one probe, in the shape the checks ask about.
type verdict struct {
	// allowed is true when the door issued a token.
	allowed bool
	// decision is the door's verdict on a refusal — empty when allowed.
	decision mantlekeep.Decision
}

func (v verdict) refusedForNoGrant() bool {
	return v.decision.Category == mantlekeep.DenialActionNotAllowed
}

func (v verdict) awaitingAPerson() bool {
	return v.decision.Action == mantlekeep.ActionRequireApproval
}

// describe is how a refusal appears inside a failure message: the decision, what refused it,
// and why, so a reader does not have to run the probe again to see what happened.
func (v verdict) describe() string {
	if v.allowed {
		return "ALLOWED (a token was issued)"
	}
	return fmt.Sprintf("%s at %q (%s): %s", v.decision.Action, v.decision.Step,
		v.decision.Category, v.decision.Reason)
}

// probe submits one intent as one subject and reads the door's answer.
//
// extraParams are merged over the action's own params into a COPY, so the deployment's Action
// values are never mutated — a suite that edited its caller's map would change what the next
// check is testing.
func (deployment Deployment) probe(t *testing.T, subject mantlekeep.Subject, action Action,
	extraParams map[string]any) verdict {

	t.Helper()
	token, err := deployment.Door.Submit(context.Background(), mantlekeep.Intent{
		ID: fmt.Sprintf("INT-GOVTEST-%d", probeCount.Add(1)),
		// Only the id is sent. The door resolves the subject — roles and IsAI — against its
		// own directory and ignores what a caller claims, which is what stops a probe from
		// proving a seal by asserting its way into the role the seal guards.
		Subject:     mantlekeep.Subject{ID: subject.ID},
		Action:      action.Name,
		Resource:    "governancetest/" + action.Name,
		Spec:        mantlekeep.IntentSpec{Goal: "governance conformance probe: " + action.Name},
		Params:      paramsWith(action.Params, extraParams),
		SubmittedAt: time.Now().UTC(),
	})
	if err == nil {
		if token.Value == "" {
			t.Fatalf("the door allowed %q for %q and issued an EMPTY token — an allow that "+
				"grants no capability cannot be acted on, and this is a door defect rather "+
				"than a policy one", action.Name, subject.ID)
		}
		return verdict{allowed: true}
	}
	var refused *mantlekeep.DecisionError
	if !errors.As(err, &refused) {
		// Not a decision at all: the engine or the chain failed. Reporting it as a refusal
		// would tell an operator their policy denied something nobody ever ruled on.
		t.Fatalf("submitting %q as %q did not produce a decision: %v — the door itself failed, "+
			"so nothing below this can be read as a policy finding", action.Name, subject.ID, err)
	}
	return verdict{decision: refused.Decision}
}

// paramsWith copies base and merges overrides onto it, always returning a non-nil map so a
// caller can add to it without a nil-map panic.
func paramsWith(base, overrides map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(overrides))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

// humanSubjects are the subjects the deployment did NOT mark IsAI — the only ones that can
// satisfy an approval gate, and so the only ones worth pointing at one.
func (deployment Deployment) humanSubjects() []mantlekeep.Subject {
	var humans []mantlekeep.Subject
	for _, subject := range deployment.Subjects {
		if !subject.IsAI {
			humans = append(humans, subject)
		}
	}
	return humans
}

// gatedActionsFirst orders the declared actions so a check that wants a gated one finds it
// without a second loop, and still has the ungated ones to fall back on.
func (deployment Deployment) gatedActionsFirst() []Action {
	ordered := make([]Action, 0, len(deployment.Actions))
	for _, action := range deployment.Actions {
		if action.Gated {
			ordered = append(ordered, action)
		}
	}
	for _, action := range deployment.Actions {
		if !action.Gated {
			ordered = append(ordered, action)
		}
	}
	return ordered
}
