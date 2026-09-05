// Package orchestrator is the spine: it executes a governed DAG under a valid
// ExecutionToken. Steps run in dependency order; independent steps run
// concurrently; a failure triggers saga compensation of everything already done;
// every transition is written to an event store. Embedded (in-process) transport
// for the MVP — the same Engine drives a NATS worker fleet later via StepRunner.
//
// Near-zero deps: this package imports stdlib only.
package orchestrator

import (
	"fmt"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Layers groups a DAG's steps into dependency levels: every step in level i
// depends only on steps in levels < i, so steps sharing a level are independent
// and may run concurrently. Order within a level follows input order (stable,
// for deterministic scheduling and tests).
//
// It returns an error when a step names an unknown dependency, a step name is
// duplicated, or the graph contains a cycle. Declare-before-execute: a workflow
// that cannot be ordered is rejected before any step runs.
func Layers(dag mantlekeep.DAG) ([][]mantlekeep.Step, error) {
	graph, err := newDependencyGraph(dag)
	if err != nil {
		return nil, err
	}

	// Kahn's algorithm, one whole frontier (level) at a time.
	var levels [][]mantlekeep.Step
	remaining := len(dag.Steps)
	for remaining > 0 {
		frontier := graph.readyNow(dag.Steps)
		if len(frontier) == 0 {
			// Steps are left but none is ready, so every one of them is waiting on another
			// that is also waiting — the definition of a cycle.
			return nil, fmt.Errorf("workflow has a cycle: %d step(s) cannot be ordered", remaining)
		}
		levels = append(levels, graph.consume(frontier))
		remaining -= len(frontier)
	}
	return levels, nil
}

// dependencyGraph is a DAG indexed for ordering: every step by name, how many dependencies
// each is still waiting on, and which steps each one unblocks when it completes.
type dependencyGraph struct {
	byName    map[string]mantlekeep.Step
	waitingOn map[string]int      // step → unsatisfied dependency count; -1 once consumed
	unblocks  map[string][]string // step → the steps that depend on it
}

// newDependencyGraph indexes the DAG and rejects the two shapes that cannot be ordered at
// all: a duplicated step name (which name would a dependency mean?) and a dependency on a
// step that does not exist. Declare-before-execute — both are refused before any step runs.
func newDependencyGraph(dag mantlekeep.DAG) (*dependencyGraph, error) {
	g := &dependencyGraph{
		byName:    make(map[string]mantlekeep.Step, len(dag.Steps)),
		waitingOn: make(map[string]int, len(dag.Steps)),
		unblocks:  make(map[string][]string),
	}
	for _, s := range dag.Steps {
		if _, dup := g.byName[s.Name]; dup {
			return nil, fmt.Errorf("duplicate step %q", s.Name)
		}
		g.byName[s.Name] = s
		g.waitingOn[s.Name] = 0
	}
	for _, s := range dag.Steps {
		for _, dep := range s.DependsOn {
			if _, ok := g.byName[dep]; !ok {
				return nil, fmt.Errorf("step %q depends on unknown step %q", s.Name, dep)
			}
			g.waitingOn[s.Name]++
			g.unblocks[dep] = append(g.unblocks[dep], s.Name)
		}
	}
	return g, nil
}

// readyNow returns the steps whose dependencies are all satisfied. It walks the DAG's INPUT
// order, not the maps, because map iteration order is random and a level's order must be
// deterministic for scheduling and tests.
func (g *dependencyGraph) readyNow(steps []mantlekeep.Step) []string {
	var frontier []string
	for _, s := range steps {
		if g.waitingOn[s.Name] == 0 {
			frontier = append(frontier, s.Name)
		}
	}
	return frontier
}

// consume takes a whole frontier as one level and releases what it unblocks.
//
// The two passes are not interchangeable. Every consumed step is marked -1 BEFORE any
// dependant is decremented, so a step already taken can never be counted down to 0 again
// and re-enter a later frontier — which would run it twice.
func (g *dependencyGraph) consume(frontier []string) []mantlekeep.Step {
	level := make([]mantlekeep.Step, 0, len(frontier))
	for _, name := range frontier {
		level = append(level, g.byName[name])
		g.waitingOn[name] = -1 // consumed — never re-enters the frontier
	}
	for _, name := range frontier {
		for _, dependant := range g.unblocks[name] {
			if g.waitingOn[dependant] > 0 {
				g.waitingOn[dependant]--
			}
		}
	}
	return level
}
