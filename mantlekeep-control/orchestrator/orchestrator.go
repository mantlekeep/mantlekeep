package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Engine implements mantlekeep.WorkflowRunner over an embedded transport. It refuses
// to run without a valid ExecutionToken (no token, nothing executes), runs steps
// in dependency order with independent steps concurrent, and on the first failure
// compensates completed steps in reverse completion order (saga). Every
// transition is written to the EventStore.
type Engine struct {
	runner StepRunner
	events EventStore
	now    func() time.Time
}

// The doc comment above claims Engine implements the core's WorkflowRunner; this makes
// the claim enforceable, so a signature drift is a compile error rather than a stale
// comment. (Same guard the registry uses for its Fetcher adapters.)
var _ mantlekeep.WorkflowRunner = (*Engine)(nil)

// NewEngine wires a step runner and an event store into the spine.
func NewEngine(runner StepRunner, events EventStore) *Engine {
	return &Engine{
		runner: runner,
		events: events,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Run implements mantlekeep.WorkflowRunner.
func (e *Engine) Run(ctx context.Context, token mantlekeep.ExecutionToken, dag mantlekeep.DAG) (mantlekeep.RunResult, error) {
	res := mantlekeep.RunResult{DAGName: dag.Name, StartedAt: e.now()}

	// The token is the gate. No valid capability → the spine does nothing.
	if token.Value == "" || !token.Valid(e.now()) {
		res.FinishedAt = e.now()
		return res, fmt.Errorf("execution token invalid or expired — refusing to run %q", dag.Name)
	}

	levels, err := Layers(dag)
	if err != nil {
		res.FinishedAt = e.now()
		return res, fmt.Errorf("workflow rejected: %w", err)
	}

	e.emit(ctx, dag.Name, "", RunStarted, "intent "+token.IntentID)

	run := e.runLevels(ctx, dag.Name, levels)

	if run.failed != "" {
		e.compensate(ctx, dag.Name, run.completed)
		res.Success, res.FailedStep, res.FinishedAt = false, run.failed, e.now()
		e.emit(ctx, dag.Name, run.failed, RunFailed, run.failErr.Error())
		return res, fmt.Errorf("workflow %q failed at step %q: %w", dag.Name, run.failed, run.failErr)
	}

	res.Success, res.FinishedAt = true, e.now()
	e.emit(ctx, dag.Name, "", RunSucceeded, "")
	return res, nil
}

// levelRun is the shared state a level's concurrent steps fold their outcome into.
// mu guards every field below it.
type levelRun struct {
	mu        sync.Mutex
	completed []mantlekeep.Step // in completion order — reversed for compensation
	failed    string
	failErr   error
}

// runLevels executes the levels in dependency order, the steps WITHIN a level
// concurrently, and stops advancing at the first level that produced a failure.
//
// Reading run.failed here needs no lock: wg.Wait has returned, so every goroutine that
// could write it has finished.
func (e *Engine) runLevels(ctx context.Context, dagName string, levels [][]mantlekeep.Step) *levelRun {
	run := &levelRun{}
	for _, level := range levels {
		var wg sync.WaitGroup
		for _, step := range level {
			wg.Add(1)
			go func(step mantlekeep.Step) {
				defer wg.Done()
				e.runStep(ctx, dagName, step, run)
			}(step)
		}
		wg.Wait()
		if run.failed != "" {
			break // stop advancing to the next level
		}
	}
	return run
}

// runStep runs ONE step and folds its outcome into the run.
//
// The event and the state change are made under the SAME lock, deliberately: a step's
// StepSucceeded event and its position in the completion order must not be interleaved
// with another step's, because that order is what compensation walks backwards.
//
// Only the FIRST failure is kept. Steps in a level run concurrently, so several may fail;
// the run is named after the one that failed first.
func (e *Engine) runStep(ctx context.Context, dagName string, step mantlekeep.Step, run *levelRun) {
	e.emit(ctx, dagName, step.Name, StepStarted, step.Runtime)
	detail, rerr := e.runner.Run(ctx, step)

	run.mu.Lock()
	defer run.mu.Unlock()
	if rerr != nil {
		e.emit(ctx, dagName, step.Name, StepFailed, rerr.Error())
		if run.failed == "" {
			run.failed, run.failErr = step.Name, rerr
		}
		return
	}
	e.emit(ctx, dagName, step.Name, StepSucceeded, detail)
	run.completed = append(run.completed, step)
}

// compensate is the saga: undo completed work, newest first. A compensation that itself
// fails is recorded and the walk CONTINUES — stopping would strand the remaining steps
// with no attempt made at all.
func (e *Engine) compensate(ctx context.Context, dagName string, completed []mantlekeep.Step) {
	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		detail, cerr := e.runner.Compensate(ctx, step)
		if cerr != nil {
			e.emit(ctx, dagName, step.Name, StepCompensated, "compensation error: "+cerr.Error())
			continue
		}
		e.emit(ctx, dagName, step.Name, StepCompensated, detail)
	}
}

func (e *Engine) emit(ctx context.Context, run, step string, kind EventKind, detail string) {
	_, _ = e.events.Append(ctx, Event{At: e.now(), Run: run, Step: step, Kind: kind, Detail: detail})
}
