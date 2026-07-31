package tooltest

import (
	"context"
	"strings"
	"testing"

	"mantlekeep.dev/control/internal/kernel"
	"mantlekeep.dev/control/registry"
)

// fakeStore is an in-memory mantlekeep.Store.
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

func fakeKernel(stdout, stderr string, exit int) *kernel.Kernel {
	return kernel.NewWithRunner("k", func(_ context.Context, _ string, _ ...string) ([]byte, []byte, int) {
		return []byte(stdout), []byte(stderr), exit
	})
}

// The money shot: a draft tool in a sealed env cannot promote until it passes a kernel
// run; running it through the kernel records the pass and unlocks ProposePromote.
func TestRun_PassUnlocksPromote(t *testing.T) {
	ctx := context.Background()
	reg := registry.New(newFakeStore(), "sit", registry.SealedProd) // requires test-before-promote
	reg.Register(ctx, "scan-tool", "tool", "Scanner", "alice", "1.0.0", "sha256:abc", nil)

	// gate closed before any test
	if _, err := reg.ProposePromote(ctx, "scan-tool", "1.0.0", "alice"); err == nil {
		t.Fatal("untested draft should not promote")
	}

	// run the draft through the kernel: ran with 2 findings → the tool executed fine
	k := fakeKernel("SOVEREIGN_SCAN_FINDINGS=2\n", "", 1)
	v, err := Run(ctx, k, reg, "scan-tool", "1.0.0", "/mod.wasm", []byte("sample"))
	if err != nil {
		t.Fatalf("tool test run: %v", err)
	}
	if !v.TestPassed || v.TestRef != "kernel scan: 2 finding(s)" {
		t.Fatalf("test should be recorded on the draft, got %+v", v)
	}

	// gate now open
	pv, err := reg.ProposePromote(ctx, "scan-tool", "1.0.0", "alice")
	if err != nil {
		t.Fatalf("promote after passing test: %v", err)
	}
	if pv.Status != registry.StatusInReview {
		t.Fatalf("want review after test+propose, got %q", pv.Status)
	}
}

func TestRun_KernelErrorKeepsGateClosed(t *testing.T) {
	ctx := context.Background()
	reg := registry.New(newFakeStore(), "sit", registry.SealedProd)
	reg.Register(ctx, "scan-tool", "tool", "Scanner", "alice", "1.0.0", "sha256:abc", nil)

	// the tool escaped the sandbox / errored → test fails, gate stays closed
	k := fakeKernel("", "SOVEREIGN_SCAN_ERROR: memory escape\n", 2)
	v, err := Run(ctx, k, reg, "scan-tool", "1.0.0", "/mod.wasm", []byte("sample"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if v.TestPassed {
		t.Fatal("a kernel error must not pass the test")
	}
	if _, err := reg.ProposePromote(ctx, "scan-tool", "1.0.0", "alice"); err == nil {
		t.Fatal("a failed kernel run must keep the promote gate closed")
	}
}
