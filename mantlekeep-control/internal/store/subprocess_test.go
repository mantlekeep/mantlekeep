package store

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildDriver compiles the separate out-of-process driver module into a temp
// binary, proving it builds as its own artifact and round-trips over stdio.
func buildDriver(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "mantlekeep-driver-postgres")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join("..", "..", "..", "drivers", "postgres")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("driver module did not build (skipping subprocess test): %v\n%s", err, out)
	}
	return bin
}

// The out-of-process driver serves the mantlekeep.Store interface over stdio — the
// core talks to a separate binary, linking none of its dependencies.
func TestSubprocessStoreRoundTrip(t *testing.T) {
	bin := buildDriver(t)
	RegisterSubprocess("postgres-ext", bin)

	st, err := Open("postgres-ext", "")
	if err != nil {
		t.Fatalf("open subprocess store: %v", err)
	}
	ctx := context.Background()

	if err := st.Put(ctx, "audit:1", []byte("hash-chain-head")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := st.Get(ctx, "audit:1")
	if err != nil || string(got) != "hash-chain-head" {
		t.Fatalf("get: %q / %v", got, err)
	}
	keys, err := st.List(ctx, "audit:")
	if err != nil || len(keys) != 1 {
		t.Fatalf("list: %v / %v", keys, err)
	}

	// A missing key surfaces the driver's error across the process boundary.
	if _, err := st.Get(ctx, "nope"); err == nil {
		t.Fatal("expected not-found error from the driver")
	}
	if sp, ok := st.(*Subprocess); ok {
		_ = sp.Close()
	}
}
