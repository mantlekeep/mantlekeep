package pgpolicy

import (
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// Policy is the Postgres-backed policy source: one type that is both a [grants.Loader] and a
// [grants.Writer] over one [Store].
//
// One type rather than two because a database source is genuinely both — unlike a file
// source, which every replica reads and none writes. A deployment that wants only the read
// half simply passes it where a Loader is wanted; the port it satisfies at the call site is
// what limits it.
type Policy struct {
	store Store
}

// Compile-time proof that this adapter satisfies the ports it claims. Without these, a change
// to either interface in the core would be discovered by whoever wired it up rather than by
// the build.
var (
	_ grants.Loader = (*Policy)(nil)
	_ grants.Writer = (*Policy)(nil)
)

// New returns a Policy over store.
//
// Panics on a nil store: a policy source with nothing behind it is a wiring error, and it is
// far better caught at startup than on the first governed change — where it would surface as
// a policy change that was approved and then lost.
func New(store Store) *Policy {
	if store == nil {
		panic("pgpolicy: New requires a non-nil Store")
	}
	return &Policy{store: store}
}
