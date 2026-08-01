package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	mantlekeep "mantlekeep.dev/control"
)

// Engine implements mantlekeep.WorkflowEngine over an embedded transport. It refuses
// to run without a valid ExecutionToken (no token, nothing executes), runs steps
// in dependency order with independent steps concurrent, and on the first failure
// compensates completed steps in reverse completion order (saga). Every
// transition is written to the EventStore.
type Engine struct {
	runner StepRunner
	events EventStore
	level  RecordingLevel
	now    func() time.Time
}

// NewEngine wires a step runner and an event store into the spine. The recording level
// defaults to RecordSteps — the full per-step timeline the engine has always emitted — so
// existing callers are unchanged. Use WithRecording to scale it down for a lighter run.
func NewEngine(runner StepRunner, events EventStore) *Engine {
	return &Engine{
		runner: runner,
		events: events,
		level:  RecordSteps,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// WithRecording sets how much of the run timeline is persisted (see RecordingLevel). It
// changes only what is RECORDED, never whether a step is governed — the token gate and the
// door decision are unaffected at every level. Returns the receiver to chain.
func (e *Engine) WithRecording(level RecordingLevel) *Engine {
	e.level = level
	return e
}

// Run implements mantlekeep.WorkflowEngine.
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

	var (
		mu        sync.Mutex
		completed []mantlekeep.Step // in completion order — reversed for compensation
		failed    string
		failErr   error
	)

	for _, level := range levels {
		var wg sync.WaitGroup
		for _, step := range level {
			wg.Add(1)
			go func(step mantlekeep.Step) {
				defer wg.Done()
				e.emit(ctx, dag.Name, step.Name, StepStarted, step.Runtime)
				detail, rerr := e.runner.Run(ctx, step)

				mu.Lock()
				defer mu.Unlock()
				if rerr != nil {
					e.emit(ctx, dag.Name, step.Name, StepFailed, rerr.Error())
					if failed == "" {
						failed, failErr = step.Name, rerr
					}
					return
				}
				e.emit(ctx, dag.Name, step.Name, StepSucceeded, detail)
				completed = append(completed, step)
			}(step)
		}
		wg.Wait()
		if failed != "" {
			break // stop advancing to the next level
		}
	}

	if failed != "" {
		// Saga: undo completed work, newest first.
		for i := len(completed) - 1; i >= 0; i-- {
			s := completed[i]
			detail, cerr := e.runner.Compensate(ctx, s)
			if cerr != nil {
				e.emit(ctx, dag.Name, s.Name, StepCompensated, "compensation error: "+cerr.Error())
				continue
			}
			e.emit(ctx, dag.Name, s.Name, StepCompensated, detail)
		}
		res.Success, res.FailedStep, res.FinishedAt = false, failed, e.now()
		e.emit(ctx, dag.Name, failed, RunFailed, failErr.Error())
		return res, fmt.Errorf("workflow %q failed at step %q: %w", dag.Name, failed, failErr)
	}

	res.Success, res.FinishedAt = true, e.now()
	e.emit(ctx, dag.Name, "", RunSucceeded, "")
	return res, nil
}

func (e *Engine) emit(ctx context.Context, run, step string, kind EventKind, detail string) {
	// Recording is the tunable axis: at `none`/`decisions` no per-step timeline is kept.
	// The door decision is already on the chain regardless — suppressing the timeline here
	// never suppresses governance, only its durable trail (docs/recording-levels.md).
	if !e.level.recordsTimeline() {
		return
	}
	_, _ = e.events.Append(ctx, Event{At: e.now(), Run: run, Step: step, Kind: kind, Detail: detail})
}
