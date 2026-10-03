package doorkit_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/app"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
)

// A person the deployment's directory knows is governed; the mock directory does not know them.
//
// This is the whole reason the constructor exists: a door built with NewDoorWithAudit resolves
// every caller against six demo names, so an embedded door refused every real person as unknown.
func TestADoorResolvesAgainstTheDeploymentsDirectory(t *testing.T) {
	intent := superAdminJob("INT-DIR", "alice", nil)

	mock, err := doorkit.NewDoorWithAudit(&countingChain{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mock.Submitter.Submit(context.Background(), intent); err == nil {
		t.Fatal("the mock directory does not know alice, so the door must refuse — the contrast " +
			"this test depends on is gone")
	}

	known := directory{"alice": {mantlekeep.RoleSuperAdmin}}
	door, err := doorkit.NewDoorWithIdentity(&countingChain{}, known)
	if err != nil {
		t.Fatalf("building the door: %v", err)
	}
	if _, err := door.Submitter.Submit(context.Background(), intent); err != nil {
		t.Fatalf("the deployment's directory knows alice as super-admin; the door refused: %v", err)
	}
	if door.Identity == nil {
		t.Error("Door.Identity does not expose the resolver the door decides with")
	}
}

// A role granted by an identity-provider GROUP reaches the decision — configured, never coded.
//
// app.BuildIdentity is what a deployment passes. With MANTLEKEEP_AUTH=proxy it maps verified groups
// to roles from MANTLEKEEP_GROUP_ROLES, so "whoever holds platform-root is root" is one line of
// configuration and the engine still names no role a deployment chose.
func TestAGroupMappedToARoleIsGovernedAsThatRole(t *testing.T) {
	t.Setenv("MANTLEKEEP_AUTH", "proxy")
	t.Setenv("MANTLEKEEP_GROUP_ROLES", "platform-root=L0-SuperAdmin")
	t.Setenv("MANTLEKEEP_TRANSPORT", "")

	door, err := doorkit.NewInMemoryDoorWithIdentity(filepath.Join(t.TempDir(), "audit.db"),
		app.BuildIdentity())
	if err != nil {
		t.Fatalf("building the door: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := door.Audit.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	if _, err := door.Submitter.Submit(context.Background(),
		superAdminJob("INT-ROOT", "alice", []string{"platform-root"})); err != nil {
		t.Fatalf("alice holds platform-root, mapped to L0-SuperAdmin; the door refused: %v", err)
	}
	if _, err := door.Submitter.Submit(context.Background(),
		superAdminJob("INT-NOGROUP", "alice", nil)); err == nil {
		t.Fatal("without the group alice holds no role; the door allowed it — roles are coming " +
			"from somewhere other than the directory")
	}
}

// A door with no identity resolver is refused, rather than quietly given the mock.
func TestADoorWithNoIdentityIsRefused(t *testing.T) {
	if _, err := doorkit.NewDoorWithIdentity(&countingChain{}, nil); err == nil {
		t.Error("NewDoorWithIdentity accepted a nil resolver")
	}
	if _, err := doorkit.NewInMemoryDoorWithIdentity(filepath.Join(t.TempDir(), "audit.db"), nil); err == nil {
		t.Error("NewInMemoryDoorWithIdentity accepted a nil resolver")
	}
}

// superAdminJob is an intent only a super-admin may run. The CLAIMED roles are deliberately empty:
// the door must decide from its directory, never from what the caller says it is.
func superAdminJob(id, subject string, groups []string) mantlekeep.Intent {
	return mantlekeep.Intent{
		ID: id, Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: subject, ADGroups: groups},
		Spec:    mantlekeep.IntentSpec{Goal: "the directory decides who this is"},
	}
}

// directory is the smallest resolver: a fixed table of people and their roles.
type directory map[string][]mantlekeep.Role

func (d directory) Resolve(_ context.Context, ext mantlekeep.ExternalIdentity) (mantlekeep.Subject, error) {
	roles, known := d[ext.ID]
	if !known {
		return mantlekeep.Subject{}, errUnknown
	}
	return mantlekeep.Subject{ID: ext.ID, Roles: roles}, nil
}

var errUnknown = errors.New("not in this directory")
