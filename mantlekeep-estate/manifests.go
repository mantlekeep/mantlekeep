package estate

import (
	"context"
	"sync"
)

// ManifestStore keeps each team's declared footprint.
//
// A read has to answer "what was declared" without asking the team to post the manifest again,
// and a reconciler has to answer it on a timer with nobody present at all. Both need the
// declaration to outlive the request that carried it.
type ManifestStore interface {
	// Remember stores a team's manifest, replacing any earlier one. The manifest IS the
	// declaration, so the newest one wins outright — merging two would produce a footprint
	// nobody wrote.
	Remember(ctx context.Context, manifest Manifest) error
	// Recall returns a team's manifest. A team that has never declared one is not an error:
	// it is a team with an empty footprint, and the caller decides what to say about that.
	Recall(ctx context.Context, team string) (Manifest, bool, error)
}

// MemoryManifests keeps manifests in memory.
//
// It is HONEST about what it is: a restart forgets every declaration, so a reconciler that
// comes back up sees an empty desired state and every live resource as unexpected. That is
// exactly why this type is named for its storage rather than called a default — a deployment
// that wants the estate to survive a restart supplies a durable [ManifestStore], and one that
// forgets to will notice on the first restart rather than on the first audit.
type MemoryManifests struct {
	mutex  sync.RWMutex
	byTeam map[string]Manifest
}

// NewMemoryManifests builds an empty in-memory store.
func NewMemoryManifests() *MemoryManifests {
	return &MemoryManifests{byTeam: map[string]Manifest{}}
}

// Remember stores the manifest under its own team name.
func (m *MemoryManifests) Remember(_ context.Context, manifest Manifest) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.byTeam[manifest.Team] = manifest
	return nil
}

// Recall returns the team's manifest, and whether there was one.
func (m *MemoryManifests) Recall(_ context.Context, team string) (Manifest, bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	manifest, found := m.byTeam[team]
	return manifest, found, nil
}
