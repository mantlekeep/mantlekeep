package pgpolicy

import (
	"context"
	"errors"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// TestADeniedPolicyChangeReachesNoRow is the reason this adapter exists at all.
//
// Somebody who can edit the grants does not need to bypass the door: they can grant
// themselves the role that opens it. Moving policy into a database makes that easier, not
// harder — which is precisely why the write path must run through [grants.Govern] and why a
// denial must reach no row. If a denied change could still land, a database-backed policy
// would be a hole in the one-door guarantee rather than a feature of it.
func TestADeniedPolicyChangeReachesNoRow(t *testing.T) {
	store := &fakeStore{}
	store.seed(t, grantsDoc(map[string][]string{"L2-Operator": {"job.run"}}), floorsDoc(nil))
	policy := New(store)

	_, _, before, err := policy.Load(context.Background())
	if err != nil {
		t.Fatalf("loading the starting policy: %v", err)
	}

	denied := errors.New("policy: deny — policy.grant requires L0-SuperAdmin")
	door := &recordingDoor{err: denied}

	_, err = grants.Govern(context.Background(), door, mantlekeep.Subject{ID: "u-17"},
		policy, policy, grants.Change{
			Role: "L2-Operator", Action: "policy.grant", Grant: true, Reason: "self-service"})

	if !errors.Is(err, denied) {
		t.Fatalf("the door's refusal did not reach the caller: %v", err)
	}
	if store.updateCalls != 0 {
		t.Errorf("a denied policy change reached the store %d times", store.updateCalls)
	}
	if _, _, after, _ := policy.Load(context.Background()); after != before {
		t.Errorf("the policy changed under a denied change: %s became %s", before, after)
	}
}

// TestAnApprovedPolicyChangeIsAppliedAndTellsTheDoorWhatItChangedFrom.
//
// The chain must record not merely that policy changed but WHICH policy became which.
// [grants.Govern] puts the before-revision on the intent; this proves the revision it puts
// there is the one this store actually served, so the chain record and the history row
// describe the same event and an auditor can join them.
func TestAnApprovedPolicyChangeIsAppliedAndTellsTheDoorWhatItChangedFrom(t *testing.T) {
	store := &fakeStore{}
	store.seed(t, grantsDoc(map[string][]string{"L2-Operator": {"job.run"}}), floorsDoc(nil))
	policy := New(store)

	_, _, before, err := policy.Load(context.Background())
	if err != nil {
		t.Fatalf("loading the starting policy: %v", err)
	}
	door := &recordingDoor{}

	after, err := grants.Govern(context.Background(), door, mantlekeep.Subject{ID: "u-17"},
		policy, policy, grants.Change{
			Role: "L2-Operator", Action: "deploy.dev", Grant: true, Reason: "onboarding the new starter"})
	if err != nil {
		t.Fatalf("an approved policy change was not applied: %v", err)
	}

	if door.intent.Action != grants.ActionGrantPolicy {
		t.Errorf("the change was submitted as %q, not as a governed policy change", door.intent.Action)
	}
	if got := door.intent.Params["revision"]; got != string(before) {
		t.Errorf("the door was told the change applied to revision %v, but the store served %s — "+
			"the chain record and the history row describe different events", got, before)
	}
	if after == before {
		t.Errorf("the revision did not move: %s", after)
	}
	if len(store.history) != 1 || store.history[0].ParentRevision != before {
		t.Errorf("the history does not record the change as applied to %s: %+v", before, store.history)
	}
}

// TestAPolicyChangeIsRefusedWhenTheStoreCannotBeRead: Govern refuses to submit a change it
// cannot state a before-revision for. Proved here through the real adapter because that is
// the combination a deployment runs — an unreachable database must fail the change, not
// submit it against an unknown policy.
func TestAPolicyChangeIsRefusedWhenTheStoreCannotBeRead(t *testing.T) {
	store := &fakeStore{headErr: errors.New("connection refused")}
	door := &recordingDoor{}

	_, err := grants.Govern(context.Background(), door, mantlekeep.Subject{ID: "u-17"},
		New(store), New(store), grants.Change{
			Role: "L2-Operator", Action: "deploy.dev", Grant: true, Reason: "onboarding"})

	if err == nil {
		t.Fatal("a policy change was applied against a store that could not be read")
	}
	if door.submits != 0 {
		t.Errorf("the door was asked to decide %d times about a change to an unknown policy", door.submits)
	}
}

// recordingDoor stands in for the door: it records what it was asked and answers as told.
type recordingDoor struct {
	intent  mantlekeep.Intent
	submits int
	err     error
}

func (d *recordingDoor) Submit(_ context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	d.submits++
	d.intent = intent
	if d.err != nil {
		return mantlekeep.ExecutionToken{}, d.err
	}
	return mantlekeep.ExecutionToken{ExpiresAt: time.Now().Add(time.Minute)}, nil
}
