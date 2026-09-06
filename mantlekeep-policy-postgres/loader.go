package pgpolicy

import (
	"context"
	"fmt"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// Load implements [grants.Loader] over the store.
//
// # An error is an error, never an empty policy
//
// Every failure path below returns a nil document set. None of them returns an empty one.
// Empty grants deny everything, so a store that failed and a store that legitimately grants
// nothing would be indistinguishable — and the first would present as a working deny-all
// rather than as the outage it is, which is the kind of failure a deployment discovers weeks
// later from a ticket queue instead of from a page.
//
// # The revision is derived, not read
//
// The returned revision comes from [grants.RevisionOf] over the documents as loaded, never
// from the row's revision column, its primary key or its timestamp. That is what makes the
// value comparable ACROSS deployments: a file deployment and this one, serving identical
// policy, report the identical revision, so a migration between them can be proved rather
// than assumed — and two replicas reporting different revisions are demonstrably serving
// different policy, which is a fact an operator can act on.
func (p *Policy) Load(ctx context.Context) (*grants.Grants, *grants.Floors, grants.Revision, error) {
	snapshot, err := p.store.Head(ctx)
	if err != nil {
		return nil, nil, "", fmt.Errorf("pgpolicy: cannot read the policy in force: %w", err)
	}

	decoded, err := decode(snapshot)
	if err != nil {
		return nil, nil, "", err
	}

	_, revision, err := encode(decoded)
	if err != nil {
		return nil, nil, "", err
	}
	return decoded.grants, decoded.floors, revision, nil
}
