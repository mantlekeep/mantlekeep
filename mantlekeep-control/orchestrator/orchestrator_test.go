package orchestrator

import (
	"context"
	"testing"
	"time"

	mantlekeep "mantlekeep.dev/control"
)

func validToken() mantlekeep.ExecutionToken {
	now := time.Now().UTC()
	return mantlekeep.ExecutionToken{Value: "tok", IntentID: "INT-T", IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
}

// stubRunner is a tiny in-test StepRunner (the PORT the Engine depends on) — the core's Engine
// tests must exercise the engine against the port, NOT the concrete LocalRunner, which now lives
// in the execution module (mantlekeep.dev/worker). Command[0]=="boom" fails; compensation records
// which steps were rolled back (Compensation[1] names the step, matching the DAGs below).
type stubRunner struct{ rolledBack *[]string }

func testRunner(rolledBack *[]string) stubRunner { return stubRunner{rolledBack: rolledBack} }

func (s stubRunner) Run(_ context.Context, step mantlekeep.Step) (string, error) {
	if len(step.Command) > 0 && step.Command[0] == "boom" {
		return "", context.Canceled // any error
	}
	return "did work", nil
}

func (s stubRunner) Compensate(_ context.Context, step mantlekeep.Step) (string, error) {
	if s.rolledBack != nil && len(step.Compensation) > 1 {
		*s.rolledBack = append(*s.rolledBack, step.Compensation[1])
	}
	return "undone", nil
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func has(evs []Event, k EventKind, step string) bool {
	for _, e := range evs {
		if e.Kind == k && (step == "" || e.Step == step) {
			return true
		}
	}
	return false
}

func TestLayersDiamond(t *testing.T) {
	dag := mantlekeep.DAG{Name: "d", Steps: []mantlekeep.Step{
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"a"}},
		{Name: "d", DependsOn: []string{"b", "c"}},
	}}
	levels, err := Layers(dag)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 3 {
		t.Fatalf("want 3 levels, got %d: %v", len(levels), levels)
	}
	if len(levels[1]) != 2 { // b and c are independent → same level
		t.Fatalf("want level 1 to hold b,c; got %v", levels[1])
	}
}

func TestLayersCycle(t *testing.T) {
	dag := mantlekeep.DAG{Name: "cyc", Steps: []mantlekeep.Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}}
	if _, err := Layers(dag); err == nil {
		t.Fatal("expected cycle rejection")
	}
}

func TestLayersUnknownDep(t *testing.T) {
	dag := mantlekeep.DAG{Name: "u", Steps: []mantlekeep.Step{{Name: "a", DependsOn: []string{"ghost"}}}}
	if _, err := Layers(dag); err == nil {
		t.Fatal("expected unknown-dependency rejection")
	}
}

func TestRunRefusesInvalidToken(t *testing.T) {
	store := NewMemStore()
	eng := NewEngine(testRunner(nil), store)
	dag := mantlekeep.DAG{Name: "x", Steps: []mantlekeep.Step{{Name: "a", Command: []string{"ok"}}}}

	expired := mantlekeep.ExecutionToken{Value: "old", ExpiresAt: time.Now().Add(-time.Minute)}
	if _, err := eng.Run(context.Background(), expired, dag); err == nil {
		t.Fatal("expected refusal on expired token")
	}
	if evs, _ := store.Events(context.Background(), ""); len(evs) != 0 {
		t.Fatalf("no steps should have run; got events %v", kinds(evs))
	}
}

func TestRunHappyPath(t *testing.T) {
	store := NewMemStore()
	eng := NewEngine(testRunner(nil), store)
	dag := mantlekeep.DAG{Name: "happy", Steps: []mantlekeep.Step{
		{Name: "a", Command: []string{"ok"}},
		{Name: "b", DependsOn: []string{"a"}, Command: []string{"ok"}},
	}}
	res, err := eng.Run(context.Background(), validToken(), dag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("want success, got %+v", res)
	}
	evs, _ := store.Events(context.Background(), "happy")
	if !has(evs, RunSucceeded, "") || !has(evs, StepSucceeded, "b") {
		t.Fatalf("missing success events: %v", kinds(evs))
	}
}

func TestRunSagaCompensatesInReverse(t *testing.T) {
	var rolledBack []string
	store := NewMemStore()
	eng := NewEngine(testRunner(&rolledBack), store)
	// a → b(boom); a completed, so a must be compensated.
	dag := mantlekeep.DAG{Name: "saga", Steps: []mantlekeep.Step{
		{Name: "a", Command: []string{"ok"}, Compensation: []string{"undo", "a"}},
		{Name: "b", DependsOn: []string{"a"}, Command: []string{"boom"}, Compensation: []string{"undo", "b"}},
	}}
	res, err := eng.Run(context.Background(), validToken(), dag)
	if err == nil {
		t.Fatal("expected failure")
	}
	if res.Success || res.FailedStep != "b" {
		t.Fatalf("want failure at b, got %+v", res)
	}
	if len(rolledBack) != 1 || rolledBack[0] != "a" {
		t.Fatalf("want only 'a' compensated (b never completed), got %v", rolledBack)
	}
	evs, _ := store.Events(context.Background(), "saga")
	if !has(evs, StepFailed, "b") || !has(evs, StepCompensated, "a") || !has(evs, RunFailed, "") {
		t.Fatalf("missing saga events: %v", kinds(evs))
	}
}
