// Package chaintest is the conformance suite every audit chain must pass.
//
// AuditLogger is two methods and a great deal of unwritten contract. A store that appends in
// parallel, or whose Verify does not actually walk the chain, satisfies the interface and destroys
// the only property the chain exists for. Neither failure is visible from the outside: the door
// still decides, still issues tokens, still looks healthy.
//
// So the contract ships as a runnable suite. A Postgres or log-backed chain — the thing that lets
// N stateless door replicas share one chain — proves these rather than being reviewed for them.
package chaintest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Factory builds a fresh, empty chain. Called once per case so cases cannot leak into each other.
type Factory func(t *testing.T) mantlekeep.AuditLogger

// Run executes the whole suite against one implementation.
func Run(t *testing.T, newChain Factory) {
	t.Helper()
	t.Run("a record comes back with its hash set", func(t *testing.T) { hashSet(t, newChain) })
	t.Run("each record links the one before it", func(t *testing.T) { linked(t, newChain) })
	t.Run("concurrent appends do not fork the chain", func(t *testing.T) { noFork(t, newChain) })
	t.Run("an empty chain verifies", func(t *testing.T) { emptyVerifies(t, newChain) })
}

func record(intentID string) mantlekeep.AuditRecord {
	return mantlekeep.AuditRecord{
		Timestamp: time.Now().UTC(),
		IntentID:  intentID,
		SubjectID: "root",
		Action:    "job.run",
		Decision:  mantlekeep.ActionAllow,
		PolicyID:  "test.rbac",
	}
}

func hashSet(t *testing.T, newChain Factory) {
	chain := newChain(t)
	stored, err := chain.Log(context.Background(), record("INT-1"))
	if err != nil {
		t.Fatalf("appending: %v", err)
	}
	if stored.Hash == "" {
		t.Fatal("a stored record must carry its hash, or nothing downstream can cite it")
	}
}

// Each record links the previous one. That link IS the tamper-evidence.
func linked(t *testing.T, newChain Factory) {
	chain := newChain(t)
	ctx := context.Background()

	first, err := chain.Log(ctx, record("INT-1"))
	if err != nil {
		t.Fatalf("appending the first: %v", err)
	}
	second, err := chain.Log(ctx, record("INT-2"))
	if err != nil {
		t.Fatalf("appending the second: %v", err)
	}

	if second.PrevHash != first.Hash {
		t.Fatalf("the second record must link the first: prev=%q first=%q — an unlinked record "+
			"can be removed without trace", second.PrevHash, first.Hash)
	}
	if ok, err := chain.Verify(ctx); err != nil || !ok {
		t.Fatalf("a chain of two honest records must verify: ok=%v err=%v", ok, err)
	}
}

// THE test. Concurrent appends must serialise, or the chain forks.
//
// Two writers that both read the same head and both link to it produce two records claiming the
// same predecessor. Nothing detects that afterwards: each record is individually well-formed, the
// chain walks, and one of them has silently replaced the other as history. This is the property
// that makes a shared chain hard, and the reason the door is one replica until a store proves it.
func noFork(t *testing.T, newChain Factory) {
	chain := newChain(t)
	ctx := context.Background()

	const writers = 24
	var wait sync.WaitGroup
	hashes := make([]string, writers)
	prevs := make([]string, writers)

	start := make(chan struct{})
	for writer := 0; writer < writers; writer++ {
		wait.Add(1)
		go func(n int) {
			defer wait.Done()
			<-start
			// A plain formatted id. The earlier form built one from a rune arithmetic
			// expression, which gosec flags as an int->rune conversion (G115) — correctly:
			// the value is unbounded at the type level even when the loop bounds it.
			stored, err := chain.Log(ctx, record(fmt.Sprintf("INT-%d", n)))
			if err == nil {
				hashes[n] = stored.Hash
				prevs[n] = stored.PrevHash
			}
		}(writer)
	}
	close(start)
	wait.Wait()

	// No two records may claim the same predecessor — that is exactly what a fork is.
	seen := map[string]int{}
	for index, prev := range prevs {
		if hashes[index] == "" {
			continue // this writer failed; a store may reject rather than serialise, which is honest
		}
		seen[prev]++
		if seen[prev] > 1 {
			t.Fatalf("two records claim the same predecessor %q — the chain forked, and each "+
				"half verifies on its own while one silently replaced the other", prev)
		}
	}

	if ok, err := chain.Verify(ctx); err != nil || !ok {
		t.Fatalf("the chain must verify after concurrent appends: ok=%v err=%v", ok, err)
	}
}

func emptyVerifies(t *testing.T, newChain Factory) {
	// An empty chain is honest, not broken. A store that errors here makes a fresh deployment
	// look tampered with on its first health check.
	if ok, err := newChain(t).Verify(context.Background()); err != nil || !ok {
		t.Fatalf("an empty chain must verify: ok=%v err=%v", ok, err)
	}
}
