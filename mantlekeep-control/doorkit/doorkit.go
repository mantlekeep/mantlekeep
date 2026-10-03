// Package doorkit assembles a real one-door (identity + pure-Go RBAC policy + audit) and
// opens its companion stores, as PUBLIC core API.
//
// It exists so a downstream product module (e.g. mantlekeep-portal) can build a genuine
// governed door for its own integration tests — or a lightweight embedding — WITHOUT
// importing core's internal packages or the app package. The app package depends on the
// portal, so a portal-module test importing app would form an illegal cross-module cycle.
// doorkit sits BELOW app and imports no product, so it is the portal-independent assembly
// seam: core's internal engine reached through a small, stable public surface.
package doorkit

import (
	"context"
	"errors"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/audit"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/identity"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/sdk"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/store"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/orchestrator"
)

// Failsafe is the read-only-mode control on an assembled door: trip it and the door serves
// a safe read-only policy; reset restores normal governance. Its method set matches the
// failsafe a portal mounts, so a *Door.Failsafe passes straight into a portal.
type Failsafe interface {
	Trip()
	Reset()
	Tripped() bool
}

// Door is an assembled one-door and the handles a caller wires into a portal or product:
// the Submitter to govern intents, the identity resolver, the hash-chained audit log, and
// the failsafe control.
type Door struct {
	Submitter mantlekeep.Submitter        // the door — POST intents here
	Identity  mantlekeep.IdentityResolver // who the door resolves callers against: the mock, or the deployment's
	Audit     mantlekeep.AuditLogger      // the durable, hash-chained audit log
	Failsafe  Failsafe                    // the read-only failsafe control
}

// NewInMemoryDoor builds a door with a MOCK identity, pure-Go RBAC policy, and a bbolt
// audit log at auditPath. Pass dyn (e.g. a product registry) to give product actions their
// RunAs authorization — it is folded in as the RBAC's dynamic action layer, the same way
// the real server wires the product registry into the door.
func NewInMemoryDoor(auditPath string, dyn ...policy.ActionAuthorizer) (*Door, error) {
	return NewInMemoryDoorWithIdentity(auditPath, identity.NewMock(), dyn...)
}

// NewInMemoryDoorWithIdentity is [NewInMemoryDoor] with the deployment's own identity resolver
// instead of the mock. See [NewDoorWithIdentity] for why the resolver has to come in here.
func NewInMemoryDoorWithIdentity(auditPath string, ids mantlekeep.IdentityResolver,
	dyn ...policy.ActionAuthorizer) (*Door, error) {

	if ids == nil {
		return nil, errNoIdentity
	}
	aud, err := audit.Open(auditPath)
	if err != nil {
		return nil, err
	}
	return NewDoorWithIdentity(aud, ids, dyn...)
}

// NewDoorWithAudit assembles a door over an audit chain the DEPLOYMENT supplies.
//
// # Why this exists
//
// NewInMemoryDoor opens a bbolt file, and bbolt takes an exclusive lock on it. That is correct for
// one process and it is what fixes the door at a single replica: a second pod does not share the
// volume, it fails to open it. The door is otherwise stateless — a decision is a pure function of
// intent, policy and floor — so the replica limit comes entirely from where the chain lives.
//
// AuditLogger has always been the seam. Nothing could reach it, because the only constructor
// hardcoded the bbolt store. This is that constructor, and it is the whole change: a chain backed
// by a database that serialises appends lets N stateless pods share one chain.
//
// What a conforming logger must guarantee, and it is not negotiable:
//
//   - appends are SERIALISED. Each record links the previous record's hash, so two concurrent
//     writers fork the chain, and a forked chain proves nothing. A store that appends in parallel
//     is not an audit chain, whatever else it is.
//   - Verify walks the whole chain and detects a break. A logger that returns true without
//     checking turns tamper-evidence into a claim.
//
// A nil logger is refused rather than defaulted. A door that records nothing still decides, still
// returns tokens, and still looks healthy — while producing no evidence that anything was governed.
// That failure is silent and total, so it fails here instead.
func NewDoorWithAudit(chain mantlekeep.AuditLogger, dyn ...policy.ActionAuthorizer) (*Door, error) {
	return NewDoorWithIdentity(chain, identity.NewMock(), dyn...)
}

// errNoIdentity refuses a door with no identity resolver, for the same reason a door with no chain
// is refused: it would look healthy and refuse every caller as unknown, with nothing saying why.
var errNoIdentity = errors.New("doorkit: no identity resolver — every intent would be refused " +
	"as an unknown subject. Pass the deployment's resolver (app.BuildIdentity reads it from " +
	"MANTLEKEEP_AUTH and MANTLEKEEP_GROUP_ROLES), or use NewDoorWithAudit for the mock")

// NewDoorWithIdentity assembles a door over a chain AND an identity resolver the deployment
// supplies.
//
// # Why this exists
//
// The door resolves every intent's roles SERVER-SIDE, from its own directory: a caller's claimed
// roles are never read (internal/sdk). That is what stops role forgery — and it means the
// directory decides who anybody is. [NewDoorWithAudit] builds that directory as the six-name
// MOCK, so a door embedded in a real deployment resolves its real users against demo names and
// answers "unknown subject" to all of them.
//
// A product that embeds the door (one process, no door to dial) therefore had no way to govern a
// real person. This takes the resolver from the deployment — typically [app.BuildIdentity], which
// maps verified identity-provider groups to roles from configuration — and leaves every other
// part of the assembly unchanged.
//
// A nil resolver is refused rather than defaulted to the mock: a deployment that meant to pass its
// own and passed nil would otherwise govern against demo names and believe it was governing.
func NewDoorWithIdentity(chain mantlekeep.AuditLogger, ids mantlekeep.IdentityResolver,
	dyn ...policy.ActionAuthorizer) (*Door, error) {

	if ids == nil {
		return nil, errNoIdentity
	}
	if chain == nil {
		return nil, errors.New("doorkit: no audit chain — a door that records nothing would " +
			"decide, issue tokens and look healthy while proving nothing was ever governed")
	}
	rbac := policy.NewRBAC()
	if len(dyn) > 0 && dyn[0] != nil {
		rbac = rbac.WithDynamic(dyn[0])
	}
	fs := policy.NewFailsafe(rbac)
	return &Door{
		Submitter: sdk.New(ids, fs, chain),
		Identity:  ids,
		Audit:     chain,
		Failsafe:  fs,
	}, nil
}

// EnsureLoaded eagerly loads + validates the merged policy (baseline ∪ platform ∪ products) from
// the configured sources (MANTLEKEEP_PLATFORM_POLICY, MANTLEKEEP_POLICY_DIR), failing fast on a malformed
// doc or a sealed-action violation. A downstream module's test (TestMain) calls this after setting
// the policy env, so an assembled door has its grants before the first governed request — without
// importing core's internal policy package.
func EnsureLoaded() { policy.EnsureLoaded() }

// ReloadPolicy re-reads the grant and floor documents through source and installs them if they
// are valid, returning the revision now in force and whether it changed.
//
// The public seam for the same mechanism door.go starts automatically: a product that drives
// policy from its own store — an onboarding register a platform team edits through a UI, a
// bundle it pulls on a signal — calls this instead of polling files, and gets the identical
// guarantee. A source that fails installs NOTHING and the last good documents keep deciding.
//
// A restart is never required to change who may do what. That is the point: governance that can
// only change on a release cadence is governance that gets switched off during an incident.
func ReloadPolicy(ctx context.Context, source grants.Loader) (inForce grants.Revision, changed bool, err error) {
	return policy.ReloadGrants(ctx, source)
}

// PolicyRevisionInForce is the revision of the documents the engine accepted and is deciding
// against — not the ones on disk. Pair it with ReloadPolicy to report what a refused reload left
// running.
func PolicyRevisionInForce() grants.Revision { return policy.RevisionInForce() }

// OpenBoltStore opens a durable, bbolt-backed key/value Store at path — the store a loop
// hub (or any component keyed on mantlekeep.Store) persists into. bbolt is file-durable, so a
// fresh process reopening the same path restores what was written.
func OpenBoltStore(path string) (mantlekeep.Store, error) {
	return store.OpenBolt(path)
}

// OpenBoltEvents opens a durable, bbolt-backed workflow EventStore at path — the run-history
// store the orchestration engine writes each step's timeline into, so a run survives a restart.
// Returned as the public orchestrator.EventStore so a product module wires it into the engine
// without importing core's internal store package.
func OpenBoltEvents(path string) (orchestrator.EventStore, error) {
	return store.OpenBoltEvents(path)
}
