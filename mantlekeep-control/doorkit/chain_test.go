package doorkit_test

import (
	"context"
	"path/filepath"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/chaintest"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
)

// The shipped bbolt chain must pass the contract it implements.
//
// It reaches the store through the public door, because that is how a deployment reaches it —
// testing the internal type directly would prove something nobody can use.
func TestTheShippedChainConformsToTheContract(t *testing.T) {
	chaintest.Run(t, func(t *testing.T) mantlekeep.AuditLogger {
		door, err := doorkit.NewInMemoryDoor(filepath.Join(t.TempDir(), "audit.db"))
		if err != nil {
			t.Fatalf("assembling the door: %v", err)
		}
		t.Cleanup(func() {
			if closer, ok := door.Audit.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
		})
		return door.Audit
	})
}

// A door can be built over a chain the deployment supplies — the seam that makes N replicas
// possible, because the replica limit comes entirely from where the chain lives.
func TestADoorCanBeBuiltOverASuppliedChain(t *testing.T) {
	supplied := &countingChain{}
	door, err := doorkit.NewDoorWithAudit(supplied)
	if err != nil {
		t.Fatalf("building the door: %v", err)
	}

	if _, err := door.Submitter.Submit(context.Background(), mantlekeep.Intent{
		ID: "INT-1", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "the supplied chain is the one that records"},
	}); err != nil {
		t.Fatalf("a super-admin job.run should be allowed: %v", err)
	}

	// The point: the door wrote to the SUPPLIED chain, not to one of its own.
	if supplied.appends == 0 {
		t.Fatal("the door recorded nowhere the deployment can see — the seam is not wired")
	}
}

// A door with no chain is refused, rather than defaulting to one.
//
// It would decide, issue tokens and look healthy while proving nothing was ever governed. That
// failure is silent and total, so it must happen at construction.
func TestADoorWithNoChainIsRefused(t *testing.T) {
	if _, err := doorkit.NewDoorWithAudit(nil); err == nil {
		t.Fatal("a door with no audit chain must be refused at construction")
	}
}

// countingChain is the smallest logger that satisfies the port, and counts what it was asked to do.
type countingChain struct {
	appends int
	last    string
}

func (c *countingChain) Log(_ context.Context, rec mantlekeep.AuditRecord) (mantlekeep.AuditRecord, error) {
	c.appends++
	rec.PrevHash = c.last
	rec.Hash = rec.IntentID + "-hash"
	c.last = rec.Hash
	return rec, nil
}

func (c *countingChain) Verify(context.Context) (bool, error) { return true, nil }
