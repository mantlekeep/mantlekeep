package orchestrator

import (
	"context"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// StepRunner is the execution PORT the Engine depends on — the seam between DECIDING/sequencing
// (this generic core) and EXECUTING (a deployable's runner). The Engine sequences a DAG and calls
// Run/Compensate; it holds NO knowledge of HOW a step runs. The concrete in-process implementation
// (builtin handlers, the sovereign kernel, OCI containers) lives OUTSIDE the core in the execution
// module (mantlekeep.dev/worker, *worker.LocalRunner); a NATS-backed coordinator (mantlekeep.dev/nats) is
// another implementation. Keeping only the port here is govern-never-execute made structural: the
// generic engine can never run work inline, because it has nothing that does.
type StepRunner interface {
	Run(ctx context.Context, step mantlekeep.Step) (detail string, err error)
	Compensate(ctx context.Context, step mantlekeep.Step) (detail string, err error)
}
