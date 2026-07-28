package registry

import (
	"context"
	"sort"
)

// Pin records that a consumer (a flow/flow) depends on a specific version — the
// reverse index that makes a deprecation's blast radius visible before you pull it.
func (r *Registry) Pin(ctx context.Context, name, version, consumer string) error {
	return r.store.Put(ctx, pinKey(name, version, consumer), []byte(consumer))
}

// Unpin drops a consumer's dependency (e.g. after it migrates off a version). The
// Store port has no Delete, so this writes a tombstone that Dependents skips.
func (r *Registry) Unpin(ctx context.Context, name, version, consumer string) error {
	return r.store.Put(ctx, pinKey(name, version, consumer), []byte(""))
}

// Dependents lists the consumers pinning name@version — the blast radius that blocks
// a demise until they migrate.
func (r *Registry) Dependents(ctx context.Context, name, version string) ([]string, error) {
	keys, err := r.store.List(ctx, pinPrefix+safeName(name)+"/"+safeName(version)+"/")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		raw, err := r.store.Get(ctx, k)
		if err != nil || len(raw) == 0 {
			continue // tombstoned / gone
		}
		out = append(out, string(raw))
	}
	sort.Strings(out)
	return out, nil
}

func pinKey(name, version, consumer string) string {
	return pinPrefix + safeName(name) + "/" + safeName(version) + "/" + safeName(consumer)
}
