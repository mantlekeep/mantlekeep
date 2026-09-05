package pgpolicy

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// TestWriteAppliesTheApprovedChangeWithoutSecondGuessingIt.
//
// By the time Write runs, the door has allowed the change. There is no branch here that may
// refuse one on its merits — a grant of something already held, or a revoke of something never
// held, is applied as the no-op it is and recorded with its reason. A refusal invented in the
// writer would be a policy engine underneath the policy engine: invisible to the floor that is
// supposed to govern it, and absent from the chain entirely.
func TestWriteAppliesTheApprovedChangeWithoutSecondGuessingIt(t *testing.T) {
	cases := map[string]struct {
		start  map[string][]string
		change grants.Change
		want   map[string][]string
	}{
		"granting an action the role already holds changes nothing and is still recorded": {
			start:  map[string][]string{"L2-Operator": {"job.run"}},
			change: grants.Change{Role: "L2-Operator", Action: "job.run", Grant: true, Reason: "re-confirmed"},
			want:   map[string][]string{"L2-Operator": {"job.run"}},
		},
		"revoking an action the role never held changes nothing and is still recorded": {
			start:  map[string][]string{"L2-Operator": {"job.run"}},
			change: grants.Change{Role: "L2-Operator", Action: "deploy.prod", Grant: false, Reason: "tidy-up"},
			want:   map[string][]string{"L2-Operator": {"job.run"}},
		},
		"revoking from a role with no entry does not invent one": {
			start:  map[string][]string{"L2-Operator": {"job.run"}},
			change: grants.Change{Role: "L9-Nobody", Action: "deploy.prod", Grant: false, Reason: "tidy-up"},
			want:   map[string][]string{"L2-Operator": {"job.run"}},
		},
		"granting to a role with no entry creates one": {
			start:  map[string][]string{},
			change: grants.Change{Role: "L3-Approver", Action: "deploy.prod", Grant: true, Reason: "new approver"},
			want:   map[string][]string{"L3-Approver": {"deploy.prod"}},
		},
		"revoking the last action removes the role rather than leaving an empty entry": {
			start:  map[string][]string{"L2-Operator": {"job.run"}, "L3-Approver": {"deploy.prod"}},
			change: grants.Change{Role: "L2-Operator", Action: "job.run", Grant: false, Reason: "left the team"},
			want:   map[string][]string{"L3-Approver": {"deploy.prod"}},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			store.seed(t, grantsDoc(testCase.start), floorsDoc(nil))

			if _, err := New(store).Write(context.Background(), testCase.change); err != nil {
				t.Fatalf("Write refused a change the door had already allowed: %v", err)
			}
			if got := store.roleActions(t); !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("grants after the change:\n  got:  %v\n  want: %v", got, testCase.want)
			}
			if len(store.history) != 1 {
				t.Fatalf("history holds %d changes, want 1 — an applied change that is not "+
					"recorded is one no audit can find", len(store.history))
			}
			if store.history[0].Change != testCase.change {
				t.Errorf("history recorded %+v, want %+v", store.history[0].Change, testCase.change)
			}
		})
	}
}

// TestWriteLeavesTheFloorDocumentAlone. A grant change says nothing about the floor, so the
// floor must come back byte-identical — and it must still be covered by the revision, because
// a deployment that changed a floor and kept its grants has changed its policy.
func TestWriteLeavesTheFloorDocumentAlone(t *testing.T) {
	floors := floorsDoc(map[string][]grants.FloorRule{
		"deploy.prod": {{Kind: "allowlist", Param: "env", Values: []string{"prod"}}},
	})
	store := &fakeStore{}
	store.seed(t, grantsDoc(map[string][]string{"L2-Operator": {"job.run"}}), floors)
	before := string(store.head.FloorsDoc)

	if _, err := New(store).Write(context.Background(), grants.Change{
		Role: "L2-Operator", Action: "deploy.dev", Grant: true, Reason: "onboarding"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if after := string(store.head.FloorsDoc); after != before {
		t.Errorf("the floor document changed under a grant change:\n  before: %s\n  after:  %s", before, after)
	}
}

// TestWriteRefusesToChangeAPolicyItCouldNotRead. Applying an approved change onto policy that
// could not be read would be writing over something unknown.
func TestWriteRefusesToChangeAPolicyItCouldNotRead(t *testing.T) {
	unreachable := errors.New("dial tcp 10.0.0.7:5432: connect: connection refused")
	store := &fakeStore{headErr: unreachable}

	_, err := New(store).Write(context.Background(), grants.Change{
		Role: "L2-Operator", Action: "deploy.dev", Grant: true, Reason: "onboarding"})

	if err == nil {
		t.Fatal("Write reported success against a store it could not read")
	}
	if !errors.Is(err, unreachable) {
		t.Errorf("the error does not say what went wrong: %v", err)
	}
	if len(store.history) != 0 {
		t.Errorf("Write recorded %d changes to a policy it had not read", len(store.history))
	}
}
