package pgpolicy

import (
	"context"
	"errors"
	"testing"
)

// TestLoadReportsAFailureAsAnErrorNeverAsAnEmptyPolicy is the failure mode this adapter is
// most obliged to get right.
//
// Empty grants deny everything. A source that answered a failed read with an empty document
// would produce a deployment that denies every action and looks, from the outside, exactly
// like a deliberate deny-all: no error in the logs, no failed health check, just a control
// plane that has quietly stopped letting anyone do anything. Every failure below must be an
// error, and must not carry a document set at all.
func TestLoadReportsAFailureAsAnErrorNeverAsAnEmptyPolicy(t *testing.T) {
	unreachable := errors.New("dial tcp 10.0.0.7:5432: connect: connection refused")

	cases := map[string]struct {
		store *fakeStore
		is    error
	}{
		"the database is unreachable": {
			store: &fakeStore{headErr: unreachable},
			is:    unreachable,
		},
		"the schema is applied but no policy was ever loaded": {
			store: &fakeStore{present: false},
			is:    ErrNoPolicy,
		},
		"the stored grant document does not parse": {
			store: &fakeStore{present: true, head: Snapshot{
				GrantsDoc: []byte(`{"role_actions": [this is not json`),
				FloorsDoc: []byte(`{"floors":{}}`),
			}},
			is: ErrCorruptDocument,
		},
		"the stored floor document does not parse": {
			store: &fakeStore{present: true, head: Snapshot{
				GrantsDoc: []byte(`{"role_actions":{}}`),
				FloorsDoc: []byte(`}`),
			}},
			is: ErrCorruptDocument,
		},
		"a document column is empty": {
			store: &fakeStore{present: true, head: Snapshot{
				GrantsDoc: nil,
				FloorsDoc: []byte(`{"floors":{}}`),
			}},
			is: ErrCorruptDocument,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			loadedGrants, loadedFloors, revision, err := New(testCase.store).Load(context.Background())

			if err == nil {
				t.Fatalf("no error — the caller cannot tell this outage from a policy that "+
					"grants nothing (got grants %+v, revision %q)", loadedGrants, revision)
			}
			if !errors.Is(err, testCase.is) {
				t.Errorf("error does not identify the cause: got %v, want one wrapping %v", err, testCase.is)
			}
			if loadedGrants != nil || loadedFloors != nil {
				t.Errorf("a failed load handed back documents (grants %+v, floors %+v); a caller "+
					"that ignores the error would now be enforcing them", loadedGrants, loadedFloors)
			}
			if revision != "" {
				t.Errorf("a failed load handed back revision %q, which would be recorded on the "+
					"chain as the policy that made a decision", revision)
			}
		})
	}
}

// TestNewRefusesAPolicySourceWithNothingBehindIt: a nil store is a wiring error, and it is far
// better found at startup than on the first governed change — where it would surface as a
// change that was approved and then lost.
func TestNewRefusesAPolicySourceWithNothingBehindIt(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New(nil) returned a Policy; a source with no store behind it fails later, " +
				"in the middle of a governed change, instead of at startup")
		}
	}()
	New(nil)
}
