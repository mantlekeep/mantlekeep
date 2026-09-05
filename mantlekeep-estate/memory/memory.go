// Package memory is a [estate.Port] that provisions nothing.
//
// It exists so the control plane can be RUN and driven end to end without a Kubernetes cluster,
// a Postgres instance or a Harbor registry — the decision layer is the part under test, and it
// is the part that had no ignition. Every governed change still passes the door and still
// arrives here carrying a token the door minted; the only thing that does not happen is the
// effect.
//
// It is named for what it is. A package called `defaults` or `local` would eventually be
// deployed by someone who thought it was a fallback, and an estate that reports everything as
// provisioned while nothing exists is worse than one that reports nothing at all: it produces
// evidence for work that never happened. Anything wiring this in must say so out loud.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// Port records what it was asked to provision and reads it back as though it were real.
type Port struct {
	asset string
	team  string
	mutex sync.RWMutex
	// byKey is what this fake "provisioned", keyed exactly as the reconciler compares — so a
	// second apply of an unchanged manifest reports no drift, as a real adapter would.
	byKey map[string]estate.ObservedItem
}

var _ estate.Port = (*Port)(nil)

// New builds a fake adapter for one asset, owned by one team.
//
// Scoped to a team like the real adapters are ([deploy.New] and [provision.New] both take one),
// because Observe is asked for a team and a port that answered for every team would report
// another team's resources as this team's unexpected drift.
func New(asset, team string) *Port {
	return &Port{asset: asset, team: team, byKey: map[string]estate.ObservedItem{}}
}

// Asset names what this adapter stands in for.
func (p *Port) Asset() string { return p.asset }

// Apply records the change, under a token it refuses to do without.
//
// The empty-token check is not ceremony. It is the one property this fake shares with a real
// adapter and the one worth protecting: a port that acts on an empty token would let a change
// reach an asset with no decision behind it, and a demo built on that would prove the opposite
// of what it claims.
func (p *Port) Apply(_ context.Context, token mantlekeep.ExecutionToken, change estate.DesiredItem) error {
	if token.Value == "" {
		return fmt.Errorf(
			"memory: refusing change %q with no execution token — an adapter that acts without "+
				"one is acting with no decision behind it", change.Name)
	}
	// An EXPIRED capability is not a decision either: the door's answer had a lifetime, and
	// acting after it has run out is acting on an answer nobody would give now.
	if !token.Valid(time.Now()) {
		return fmt.Errorf("memory: refusing change %q — execution token %q expired at %s",
			change.Name, token.IntentID, token.ExpiresAt)
	}
	if change.Asset != p.asset {
		return fmt.Errorf("memory: this adapter provisions %q, not %q", p.asset, change.Asset)
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.byKey[key(change)] = estate.ObservedItem{
		Asset: change.Asset, Kind: change.Kind, Name: change.Name, Cluster: change.Cluster,
		Slot: change.Slot, Limits: change.Limits, Readers: change.Readers,
		Image: change.Image, Digest: change.Digest, Runtime: change.Runtime,
		State: change.State,
	}
	return nil
}

// Observe reads back what this adapter holds for a team.
func (p *Port) Observe(_ context.Context, team string) (estate.Observed, error) {
	if team != p.team {
		return estate.Observed{}, nil
	}
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	observed := estate.Observed{Items: make([]estate.ObservedItem, 0, len(p.byKey))}
	for _, item := range p.byKey {
		observed.Items = append(observed.Items, item)
	}
	return observed, nil
}

// PlantDrift makes this adapter report something that was never approved, so a demo can show
// the reconciler finding it. It is the ONLY way to change what this fake holds other than a
// governed apply — an out-of-band edit has to be an explicit act in the code, never a flag on
// a request.
func (p *Port) PlantDrift(item estate.ObservedItem) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.byKey[observedKey(item)] = item
}

// key mirrors the identity the reconciler compares on: the slot when there is one, the name
// otherwise. Duplicated from the engine's unexported key() rather than exported from it,
// because exporting a comparison key invites callers to build their own — and the day the two
// disagree, drift becomes invisible.
func key(change estate.DesiredItem) string {
	if !change.Slot.Empty() {
		return change.Asset + "/" + change.Kind + "/" + change.Slot.Key()
	}
	return change.Asset + "/" + change.Kind + "/" + change.Name
}

func observedKey(item estate.ObservedItem) string {
	if !item.Slot.Empty() {
		return item.Asset + "/" + item.Kind + "/" + item.Slot.Key()
	}
	return item.Asset + "/" + item.Kind + "/" + item.Name
}
