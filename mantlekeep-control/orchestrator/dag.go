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

	mantlekeep "mantlekeep.dev/control"
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
	byName := make(map[string]mantlekeep.Step, len(dag.Steps))
	indeg := make(map[string]int, len(dag.Steps))
	children := make(map[string][]string)

	for _, s := range dag.Steps {
		if _, dup := byName[s.Name]; dup {
			return nil, fmt.Errorf("duplicate step %q", s.Name)
		}
		byName[s.Name] = s
		indeg[s.Name] = 0
	}
	for _, s := range dag.Steps {
		for _, dep := range s.DependsOn {
			if _, ok := byName[dep]; !ok {
				return nil, fmt.Errorf("step %q depends on unknown step %q", s.Name, dep)
			}
			indeg[s.Name]++
			children[dep] = append(children[dep], s.Name)
		}
	}

	// Kahn's algorithm, one whole frontier (level) at a time.
	var levels [][]mantlekeep.Step
	remaining := len(dag.Steps)
	for remaining > 0 {
		var frontier []string
		for _, s := range dag.Steps { // input order → deterministic level
			if indeg[s.Name] == 0 {
				frontier = append(frontier, s.Name)
			}
		}
		if len(frontier) == 0 {
			return nil, fmt.Errorf("workflow has a cycle: %d step(s) cannot be ordered", remaining)
		}
		level := make([]mantlekeep.Step, 0, len(frontier))
		for _, name := range frontier {
			level = append(level, byName[name])
			indeg[name] = -1 // consumed — never re-enters the frontier
			remaining--
		}
		for _, name := range frontier {
			for _, ch := range children[name] {
				if indeg[ch] > 0 {
					indeg[ch]--
				}
			}
		}
		levels = append(levels, level)
	}
	return levels, nil
}
