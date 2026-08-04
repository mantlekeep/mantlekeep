// Package tooltest is the local test loop: run a DRAFT tool through the sovereign
// kernel on sample input, then record the outcome on the draft so the
// test-before-promote gate can open. It joins the kernel (execution) and the registry
// (governance) — neither depends on the other; the glue lives here.
package tooltest

import (
	"context"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/kernel"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/registry"
)

// Run scans a draft tool's module with the given input and records the result on the
// draft version. The test PASSES when the tool executed inside the sandbox without a
// kernel error (clean OR findings both mean it ran); a kernel error fails it. Returns
// the updated version — under a RequireTestBeforePromote policy, a pass now unlocks
// ProposePromote.
func Run(ctx context.Context, k *kernel.Kernel, reg *registry.Registry, name, version, modulePath string, input []byte) (registry.Version, error) {
	res, err := k.Scan(ctx, modulePath, input)
	if err != nil {
		return registry.Version{}, err
	}
	return reg.RecordTest(ctx, name, version, res.Ran, res.Summary())
}
