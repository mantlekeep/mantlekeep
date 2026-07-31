package store

import (
	"context"
	"testing"
)

// The embedded mem driver is always compiled in — no tag, no dependency.
func TestMemDriverAlwaysAvailable(t *testing.T) {
	found := false
	for _, n := range Available() {
		if n == "mem" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mem driver must always be available, got %v", Available())
	}
	st, err := Open("mem", "")
	if err != nil {
		t.Fatalf("open mem: %v", err)
	}
	if err := st.Put(context.Background(), "k", []byte("v")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, _ := st.Get(context.Background(), "k")
	if string(got) != "v" {
		t.Fatalf("get: %q", got)
	}
}

// Opening a driver that wasn't compiled in fails with a helpful, tag-naming
// message — the whole point of the opt-in build.
func TestUnbuiltDriverErrors(t *testing.T) {
	_, err := Open("cassandra", "")
	if err == nil {
		t.Fatal("expected error opening a driver that isn't built in")
	}
}
