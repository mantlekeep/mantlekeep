package estate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrUnknownTeam is returned when a team has never declared a manifest. It is a distinct error
// rather than an empty estate so a caller can tell "this team has nothing" from "this team is
// not a team" — reporting the second as the first is how a typo in a URL reads as a clean bill
// of health.
var ErrUnknownTeam = errors.New("estate: no manifest has been declared for this team")

// ErrStoreFailure wraps a failure of the manifest store itself.
//
// Separated from every other error a caller can cause, because it is the one that is nobody's
// fault but ours. A store that cannot be written is an outage, and reporting it as a bad
// request would send a team to re-read a manifest that was never the problem.
var ErrStoreFailure = errors.New("estate: the manifest store is unavailable")

// Service answers READ questions about a team's footprint. It NEVER calls the door.
//
// That separation is deliberate and matches the rest of the estate: the door records DECISIONS,
// and routing queries through it buries the decisions in traffic — an auditor reading the chain
// then has to find the handful of changes among thousands of page loads. Reads also have no
// decision to record: nothing happens as a result of one.
//
// It shares its ports with the [Manager] rather than owning a second set. Two views of the
// estate built from two port sets would eventually be built from two DIFFERENT port sets, and
// the read would stop describing the thing the writes went to.
type Service struct {
	// floorOf reads the floor in force right now, for the same reason the manager does: the
	// estate view resolves the declaration UNDER the floor, so a read side holding the boot
	// floor would show a team limits that no longer apply — and the two halves of one product
	// would disagree about the same manifest.
	floorOf   func() Floor
	ports     map[string]Port
	ownership Ownership
	// placerOf reads the fleet in force right now. A cluster going offline is the most
	// time-critical config change there is — placement keeps choosing it until something says
	// otherwise — so the registry is reloadable for the same reason the floor is.
	placerOf  func() *Placer
	manifests ManifestStore
	// now is injectable so a test asserts on a timestamp it controls.
	now func() time.Time
}

// FloorFrom makes the read side follow a reloadable floor, so the estate view and the door
// answer under the same revision.
func (s *Service) FloorFrom(provider func() Floor) *Service {
	s.floorOf = provider
	return s
}

// PlaceOnLive makes the read side follow the same reloadable fleet the manager places on, so
// the estate view and the door do not disagree about which clusters exist.
func (s *Service) PlaceOnLive(provider func() *Placer) *Service {
	s.placerOf = provider
	return s
}

// NewService wires the read side to the same floor and adapters the manager writes through.
func NewService(floor Floor, manifests ManifestStore, ports ...Port) *Service {
	byAsset := make(map[string]Port, len(ports))
	for _, port := range ports {
		byAsset[port.Asset()] = port
	}
	return &Service{floorOf: func() Floor { return floor }, placerOf: func() *Placer { return nil },
		ports: byAsset, manifests: manifests,
		ownership: DefaultOwnership(), now: time.Now}
}

// PlaceOn supplies the fleet the read side resolves against, so an estate view shows the same
// cluster the manager would choose rather than a second opinion.
func (s *Service) PlaceOn(placer *Placer) *Service {
	s.placerOf = func() *Placer { return placer }
	return s
}

// placedFor mirrors the manager's: a read must not move anything, so it resolves against where
// things already are.
func (s *Service) placedFor(team string) map[string]string {
	placed := map[string]string{}
	for _, port := range s.ports {
		observed, err := port.Observe(context.Background(), team)
		if err != nil {
			continue
		}
		for _, item := range observed.Items {
			if item.Asset == "app" && item.Slot.Cluster != "" {
				placed[shortName(item.Name, team)] = item.Slot.Cluster
			}
		}
	}
	return placed
}

// GovernFields replaces the default field ownership, so the drift a read reports is judged by
// the same rules the reconciler corrects under.
func (s *Service) GovernFields(ownership Ownership) *Service {
	s.ownership = ownership
	return s
}

// Footprint is one team's footprint: what was declared, what that resolves to under the floor,
// what actually exists, and where the two disagree.
//
// APPROVED and OBSERVED stay separate values all the way to the wire. Merging them into a
// single "current state" would be a lie the moment somebody hand-edits a resource — the merged
// value would report the hand edit as though it had been approved, which is precisely the
// question this type exists to answer.
type Footprint struct {
	Team string `json:"team"`
	// ObservedAt stamps when reality was read, not when the manifest was declared. A drift
	// report with no read time cannot be told from a stale one.
	ObservedAt time.Time `json:"observedAt"`
	Manifest   Manifest  `json:"manifest"`
	Desired    Desired   `json:"desired"`
	Observed   Observed  `json:"observed"`
	Drifts     []Drift   `json:"drifts"`
}

// Manifest returns what a team declared, or [ErrUnknownTeam].
//
// A read, so it does not go through the door — and the reconciler needs it too, to know what to
// reconcile against when nobody is present to post it.
func (s *Service) Manifest(ctx context.Context, team string) (Manifest, error) {
	manifest, found, err := s.manifests.Recall(ctx, team)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: recall %s: %v", ErrStoreFailure, team, err)
	}
	if !found {
		return Manifest{}, ErrUnknownTeam
	}
	return manifest, nil
}

// Estate reads a team's declared footprint, resolves it under the floor, observes reality, and
// reports where they disagree.
func (s *Service) Footprint(ctx context.Context, team string) (Footprint, error) {
	manifest, err := s.Manifest(ctx, team)
	if err != nil {
		return Footprint{}, err
	}

	desired, err := ResolveWith(manifest, s.floorOf(), s.placerOf(), s.placedFor(manifest.Team))
	if err != nil {
		return Footprint{}, err
	}
	observed, err := observeAll(ctx, s.ports, team)
	if err != nil {
		return Footprint{}, err
	}

	footprint := Footprint{
		Team: team, ObservedAt: s.now().UTC(), Manifest: manifest,
		Desired: desired, Observed: observed,
		Drifts: DiffOwned(desired, observed, s.ownership),
	}
	// Empty rather than null, all the way down. A reader that has to treat null and [] as the
	// same thing will one day treat one of them as "unknown" instead.
	if footprint.Desired.Changes == nil {
		footprint.Desired.Changes = []DesiredItem{}
	}
	if footprint.Observed.Items == nil {
		footprint.Observed.Items = []ObservedItem{}
	}
	if footprint.Drifts == nil {
		footprint.Drifts = []Drift{}
	}
	return footprint, nil
}

// observeAll reads every registered adapter and merges what they found.
//
// Shared by the read path and the reconciler on purpose: two copies of this loop would be two
// definitions of what "reality" means, and the reconciler would eventually correct against a
// picture the read never showed.
//
// Deterministic order, because the port map's iteration order is not. An estate whose rows
// reshuffle between two reads of an unchanged system reads as churn, and nobody compares a
// report they cannot diff with yesterday's.
func observeAll(ctx context.Context, ports map[string]Port, team string) (Observed, error) {
	var observed Observed
	for _, port := range ports {
		seen, err := port.Observe(ctx, team)
		if err != nil {
			// An asset that cannot be read is UNKNOWN, not clean. Reporting an unreadable
			// adapter as "no drift" would present a healthy estate built on a failed query.
			return Observed{}, fmt.Errorf("observe %s: %w", port.Asset(), err)
		}
		observed.Items = append(observed.Items, seen.Items...)
	}
	sort.SliceStable(observed.Items, func(i, j int) bool {
		return observed.Items[i].key() < observed.Items[j].key()
	})
	return observed, nil
}
