package registry

import "context"

// Promoter hands an approved version from one env's registry to the NEXT env's
// registry (dev→sit→uat→prod). Regulated organizations run a SEPARATE registry
// per env — often on separate networks — behind one portal, so promotion is a governed cross-instance
// handoff and the TARGET env applies its OWN (stricter) policy on ingest.
type Promoter interface {
	Promote(ctx context.Context, entry Entry, version Version) error
}

// LocalPromoter promotes into another Registry reachable in the same process (both
// env stores are local). The air-gapped prod variant is a RemotePromoter that pushes
// to another env's API and verifies the signed provenance on ingest — a later adapter.
type LocalPromoter struct {
	Target *Registry
}

// Promote registers the version into the target env as a fresh DRAFT carrying the
// SAME artifact digest (Ref) — the upper env publishes the exact bytes SIT built, it
// does NOT rebuild. Provenance is carried up and marked promotedFrom, so the chain
// shows this was promoted, not re-fetched. The target env's own gate decides whether
// it publishes there.
func (p LocalPromoter) Promote(ctx context.Context, entry Entry, version Version) error {
	reg := Registration{
		Name: entry.Name, Kind: entry.Kind, Title: entry.Title, Owner: entry.Owner,
		Version: version.Version, Ref: version.Ref, Manifest: version.Manifest,
	}
	if _, err := p.Target.Register(ctx, reg); err != nil {
		return err
	}
	prov := map[string]string{"promotedFrom": version.Env}
	for k, v := range version.Provenance {
		prov[k] = v
	}
	_, err := p.Target.transition(ctx, entry.Name, version.Version, func(v *Version) error {
		v.Provenance = prov
		return nil
	})
	return err
}

var _ Promoter = LocalPromoter{}
