package registry

import (
	"context"
	"strings"
	"testing"
)

// fakeStore is an in-memory mantlekeep.Store for tests (no bolt dependency).
type fakeStore struct{ m map[string][]byte }

func newFakeStore() *fakeStore { return &fakeStore{m: map[string][]byte{}} }

func (f *fakeStore) Get(_ context.Context, k string) ([]byte, error) { return f.m[k], nil }
func (f *fakeStore) Put(_ context.Context, k string, v []byte) error {
	f.m[k] = append([]byte(nil), v...)
	return nil
}
func (f *fakeStore) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for k := range f.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func TestRegister_DraftAndImmutableVersion(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "dev", LooseDev)

	if _, err := r.Register(ctx, Registration{Name: "scan-tool", Kind: "tool", Title: "Scanner", Owner: "alice", Version: "1.0.0", Ref: "sha256:aaa"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	e, ok, err := r.Get(ctx, "scan-tool")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if len(e.Versions) != 1 || e.Versions[0].Status != StatusDraft || e.Versions[0].Env != "dev" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	// same version again → rejected (immutable)
	if _, err := r.Register(ctx, Registration{Name: "scan-tool", Kind: "tool", Version: "1.0.0"}); err == nil {
		t.Fatal("expected duplicate-version rejection")
	}
	// a new version → appended
	if _, err := r.Register(ctx, Registration{Name: "scan-tool", Kind: "tool", Owner: "bob", Version: "1.1.0"}); err != nil {
		t.Fatalf("second version: %v", err)
	}
	e, _, _ = r.Get(ctx, "scan-tool")
	if len(e.Versions) != 2 {
		t.Fatalf("want 2 versions, got %d", len(e.Versions))
	}
}

func TestLoosePolicy_PublishesOnPropose(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "dev", LooseDev)
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "alice", Version: "1.0.0"})

	v, err := r.ProposePromote(ctx, "t", "1.0.0", "alice")
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if v.Status != StatusPublished {
		t.Fatalf("loose env should publish on propose, got %q", v.Status)
	}
}

func TestSealedPolicy_ReviewThenApprove_WithSoD(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "prod", SealedProd)
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "alice", Version: "1.0.0"})
	r.RecordTest(ctx, "t", "1.0.0", true, "run-1") // sealed env: must pass a test first

	v, err := r.ProposePromote(ctx, "t", "1.0.0", "alice")
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if v.Status != StatusInReview {
		t.Fatalf("sealed env should hold in review, got %q", v.Status)
	}
	// SoD: proposer cannot approve their own
	if _, err := r.Approve(ctx, "t", "1.0.0", "alice"); err == nil {
		t.Fatal("expected separation-of-duties rejection")
	}
	v, err = r.Approve(ctx, "t", "1.0.0", "bob")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if v.Status != StatusPublished || v.ApprovedBy != "bob" {
		t.Fatalf("want published by bob, got %+v", v)
	}
}

func TestTestBeforePromoteGate(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "sit", SealedProd) // sealed env requires a passing test
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "alice", Version: "1.0.0"})

	// promote blocked: untested draft
	if _, err := r.ProposePromote(ctx, "t", "1.0.0", "alice"); err == nil {
		t.Fatal("expected test-before-promote gate to block an untested draft")
	}
	// a failing test still doesn't unlock it
	r.RecordTest(ctx, "t", "1.0.0", false, "run-fail")
	if _, err := r.ProposePromote(ctx, "t", "1.0.0", "alice"); err == nil {
		t.Fatal("a failing test must not unlock promote")
	}
	// a passing test unlocks propose → review
	r.RecordTest(ctx, "t", "1.0.0", true, "run-pass")
	v, err := r.ProposePromote(ctx, "t", "1.0.0", "alice")
	if err != nil {
		t.Fatalf("propose after passing test: %v", err)
	}
	if v.Status != StatusInReview {
		t.Fatalf("want review after test+propose, got %q", v.Status)
	}
}

func TestLooseEnv_IgnoresTestGate(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "dev", LooseDev) // dev iterates freely, no gate
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "alice", Version: "1.0.0"})
	v, err := r.ProposePromote(ctx, "t", "1.0.0", "alice")
	if err != nil {
		t.Fatalf("loose env should not gate on test: %v", err)
	}
	if v.Status != StatusPublished {
		t.Fatalf("want published in loose dev, got %q", v.Status)
	}
}

func TestReject_BackToDraft(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "prod", SealedProd)
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "alice", Version: "1.0.0"})
	r.RecordTest(ctx, "t", "1.0.0", true, "run-1")
	r.ProposePromote(ctx, "t", "1.0.0", "alice")

	v, err := r.Reject(ctx, "t", "1.0.0", "bob")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if v.Status != StatusDraft || v.ProposedBy != "" {
		t.Fatalf("reject should reset to draft, got %+v", v)
	}
}

func TestDeprecateThenDemise_BlockedByDependents(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "prod", SealedProd)
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "alice", Version: "1.0.0"})
	r.RecordTest(ctx, "t", "1.0.0", true, "run-1")
	r.ProposePromote(ctx, "t", "1.0.0", "alice")
	r.Approve(ctx, "t", "1.0.0", "bob")

	if _, err := r.Deprecate(ctx, "t", "1.0.0"); err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	// a flow still pins it → demise blocked
	r.Pin(ctx, "t", "1.0.0", "demo-job")
	if _, err := r.Demise(ctx, "t", "1.0.0", false); err == nil {
		t.Fatal("expected demise blocked by dependent")
	}
	// consumer migrates off → demise allowed
	r.Unpin(ctx, "t", "1.0.0", "demo-job")
	v, err := r.Demise(ctx, "t", "1.0.0", false)
	if err != nil {
		t.Fatalf("demise after unpin: %v", err)
	}
	if v.Status != StatusDemised {
		t.Fatalf("want demised, got %q", v.Status)
	}
}

func TestDependents_ListsPins(t *testing.T) {
	ctx := context.Background()
	r := New(newFakeStore(), "prod", SealedProd)
	r.Register(ctx, Registration{Name: "t", Kind: "tool", Owner: "a", Version: "1.0.0"})
	r.Pin(ctx, "t", "1.0.0", "pipe-a")
	r.Pin(ctx, "t", "1.0.0", "pipe-b")

	dep, err := r.Dependents(ctx, "t", "1.0.0")
	if err != nil {
		t.Fatalf("dependents: %v", err)
	}
	if len(dep) != 2 || dep[0] != "pipe-a" || dep[1] != "pipe-b" {
		t.Fatalf("want [pipe-a pipe-b], got %v", dep)
	}
}
