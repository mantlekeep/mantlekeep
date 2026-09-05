package estate

import (
	"context"
	"fmt"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// ReadOnly wraps an adapter so it can watch an estate but never change one.
//
// # Why a wrapper rather than a flag
//
// The first use of this engine is usually not to deploy anything — it is to find out what
// is already out there: what is declared, what is actually running, what drifted, and what
// exists that nobody approved. That job needs Observe and nothing else.
//
// A configuration flag would leave Apply reachable and rely on the flag being read
// correctly on every path. Here the writing adapter is not registered at all; what is
// registered has an Apply that only refuses. There is no ordering, no precedence and no
// missed branch, because there is no code that could write.
//
// It refuses rather than silently succeeding. An Apply that returned nil would report a
// change as applied when nothing happened, which is worse than either applying or
// refusing: the estate would record a change that reality never received.
//
// The refusal names the adapter and says the deployment is read-only, so an operator who
// hits it learns the reason from the message rather than from a config file.
func ReadOnly(port Port) Port { return readOnly{port: port} }

type readOnly struct{ port Port }

func (r readOnly) Asset() string { return r.port.Asset() }

func (r readOnly) Observe(ctx context.Context, team string) (Observed, error) {
	return r.port.Observe(ctx, team)
}

func (r readOnly) Apply(_ context.Context, _ mantlekeep.ExecutionToken, change DesiredItem) error {
	return fmt.Errorf("%s: this deployment is read-only — %q was governed and recorded, but "+
		"no adapter here can change anything", r.port.Asset(), change.Name)
}
