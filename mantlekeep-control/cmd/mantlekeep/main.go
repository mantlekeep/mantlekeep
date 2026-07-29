// Command mantlekeep is the Week-1 smoke test of the one door — and the Tier-0
// proof: a single binary, no Docker, "just run it" (like v3 `java -jar`).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/internal/audit"
	"mantlekeep.dev/control/internal/identity"
	"mantlekeep.dev/control/internal/policy"
	"mantlekeep.dev/control/internal/sdk"
	"mantlekeep.dev/control/orchestrator"
)

func main() {
	// Role dispatch — the lean CORE binary: engine + governance demos only. The PRODUCT
	// surface (the Portal, the Canvas, the product catalog) is served by the MantleKeep
	// binary in the product module (mantlekeep.dev/portal), which composes this core through
	// its public builders — the core carries no product code.
	//   mantlekeep           embedded door + spine demo (Tier 0, no NATS)
	//   mantlekeep wiring    the provider registry (Spring-style: many backends, config routes)
	// The DevOps AI-AGENT role is a PRODUCT — it lives in the mantlekeep-agent module
	// (`mantlekeep-agent`), which composes this core through its public seams; the core carries no
	// product code. The DISTRIBUTED coordinator/worker roles live in the mantlekeep-nats module
	// (`mantlekeep-nats server` / `mantlekeep-nats worker`) so the core links no NATS.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "wiring":
			runWiring()
			return
		case "agent":
			fmt.Println("the \"agent\" role is a PRODUCT — run it from the mantlekeep-agent binary: `mantlekeep-agent` (composes the core through its public door seam)")
			os.Exit(2)
		case "serve":
			runServe()
			return
		case "run", "products", "publish", "install":
			fmt.Printf("the %q role is a PRODUCT command — run it from the MantleKeep binary (mantlekeep.dev/portal)\n", os.Args[1])
			os.Exit(2)
		case "server", "worker":
			fmt.Printf("the %q role is distributed — run it from the mantlekeep-nats binary: `mantlekeep-nats %s` (needs NATS_URL)\n", os.Args[1], os.Args[1])
			os.Exit(2)
		}
	}

	ctx := context.Background()

	dbPath := filepath.Join(os.TempDir(), "mantlekeep-audit.db")
	_ = os.Remove(dbPath)
	aud, err := audit.Open(dbPath)
	must(err)
	defer aud.Close()

	// Pure-Go default policy — no OPA linked into the core binary. The core ships ZERO grants
	// (the data wall): a deployable wires the policy it ships via WithProviders. Here a tiny
	// EXAMPLE provider supplies demo grants so this smoke test shows a real allow/deny mix —
	// swap it for your product's policy adapter (see docs/extending.md).
	door := sdk.New(identity.NewMock(), policy.NewRBAC().WithProviders(exampleGrants{}), aud)

	fmt.Println("🔱 MantleKeep — the one door (Week-1 smoke test)")
	fmt.Println("────────────────────────────────────────────────────────")

	cases := []mantlekeep.Intent{
		// SuperAdmin (L0) — the engine-baked wildcard → ALLOWED (needs no grant)
		{ID: "INT-001", Action: "job.run", Resource: "project/demo",
			Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
			Spec:    mantlekeep.IntentSpec{Goal: "run anything — superadmin wildcard"}},
		// Consumer runs a job → ALLOWED (granted to L3-Consumer by the example provider)
		{ID: "INT-002", Action: "job.run", Resource: "project/demo",
			Subject: mantlekeep.Subject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
			Spec:    mantlekeep.IntentSpec{Goal: "run the build"}},
		// AI agent proposes a change → ALLOWED (AI may propose; granted to AI-Agent)
		{ID: "INT-003", Action: "change.propose", Resource: "project/demo",
			Subject: mantlekeep.Subject{ID: "ci-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
			Spec:    mantlekeep.IntentSpec{Goal: "propose a dependency bump"}},
		// Intent with NO goal → DENIED (declare-before-execute)
		{ID: "INT-004", Action: "job.run", Resource: "project/demo",
			Subject: mantlekeep.Subject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
			Spec:    mantlekeep.IntentSpec{Goal: ""}},
		// AI agent tries to APPROVE → DENIED (never granted — the core ships zero grants; you supply them)
		{ID: "INT-005", Action: "change.approve", Resource: "project/demo",
			Subject: mantlekeep.Subject{ID: "ci-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
			Spec:    mantlekeep.IntentSpec{Goal: "approve the change"}},
	}

	for _, in := range cases {
		tok, err := door.Submit(ctx, in)
		if err != nil {
			fmt.Printf("  ✗ %-9s %-22s %-22s DENIED  — %v\n", in.Subject.ID, in.Action, in.ID, err)
			continue
		}
		fmt.Printf("  ✓ %-9s %-22s %-22s ALLOWED — token %s…\n", in.Subject.ID, in.Action, in.ID, tok.Value[:8])
	}

	ok, err := aud.Verify(ctx)
	must(err)
	fmt.Println("────────────────────────────────────────────────────────")
	fmt.Printf("audit hash-chain intact: %v\n", ok)

	// ── The spine — run a governed DAG under a token ─────────────────────────
	// This shows the ENGINE (orchestrator + saga), never a product: a GENERIC two-step DAG
	// (step-a → step-b) driven by a tiny inline runner. Real product DAGs live in the product
	// catalog (mantlekeep.dev/portal), and real execution lives behind WorkerPort in a worker
	// deployable — the core carries neither. We mint the token as the SuperAdmin wildcard so the
	// demo needs zero external policy data (grants are DATA the core no longer embeds).
	fmt.Println()
	fmt.Println("🔱 MantleKeep — the spine (orchestrator engine + saga)")
	fmt.Println("────────────────────────────────────────────────────────")

	token, err := door.Submit(ctx, mantlekeep.Intent{
		ID: "INT-100", Action: "spine.run", Resource: "demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "run the generic spine demo"},
	})
	must(err)
	fmt.Printf("  door issued execution token %s… for intent %s\n", token.Value[:8], token.IntentID)

	runner := demoRunner{}
	runChain(ctx, "clean run", runner, token, demoDAG(false))
	runChain(ctx, "step-b fails → saga rollback", runner, token, demoDAG(true))
}

// demoDAG is a GENERIC two-step DAG (step-a → step-b) — the core demo shows the machinery, never
// a product. Pass fail=true to make step-b fail so the engine compensates completed steps (saga).
func demoDAG(fail bool) mantlekeep.DAG {
	stepB := mantlekeep.Step{
		Name: "step-b", Runtime: "builtin", DependsOn: []string{"step-a"},
		Command:      []string{"work"},
		Compensation: []string{"undo"},
	}
	if fail {
		stepB.Command = []string{"boom"}
	}
	return mantlekeep.DAG{
		Name: "spine-demo",
		Steps: []mantlekeep.Step{
			{Name: "step-a", Runtime: "builtin", Command: []string{"work"}, Compensation: []string{"undo"}},
			stepB,
		},
	}
}

// demoRunner is a tiny inline StepRunner (the core's StepRunner PORT, satisfied here for the demo
// only). It runs the generic verbs and fails on "boom" to exercise the saga path. Production
// execution is a real adapter behind WorkerPort, in a worker deployable — never in the core.
type demoRunner struct{}

func (demoRunner) Run(_ context.Context, step mantlekeep.Step) (string, error) {
	if len(step.Command) > 0 && step.Command[0] == "boom" {
		return "", fmt.Errorf("step %q failed", step.Name)
	}
	return "did " + step.Name, nil
}

func (demoRunner) Compensate(_ context.Context, step mantlekeep.Step) (string, error) {
	if len(step.Compensation) == 0 {
		return "nothing to undo", nil
	}
	return "undid " + step.Name, nil
}

// runChain executes one DAG on a fresh event store and prints its timeline.
func runChain(ctx context.Context, label string, runner orchestrator.StepRunner, token mantlekeep.ExecutionToken, dag mantlekeep.DAG) {
	store := orchestrator.NewMemStore()
	res, err := orchestrator.NewEngine(runner, store).Run(ctx, token, dag)

	status := "✓ success"
	if !res.Success {
		status = "✗ failed at " + res.FailedStep
	}
	fmt.Printf("\n  ▶ %s: %s\n", label, status)
	if err != nil {
		fmt.Printf("    reason: %v\n", err)
	}
	evs, _ := store.Events(ctx, dag.Name)
	for _, e := range evs {
		step := e.Step
		if step == "" {
			step = "—"
		}
		fmt.Printf("    %2d  %-16s %-8s %s\n", e.Seq, e.Kind, step, e.Detail)
	}
}

// exampleGrants is a tiny EXAMPLE PolicyProvider so this smoke test shows a real allow/deny mix.
// The core ships NO grants (the data wall); a deployable supplies its policy this way. Replace it
// with your product's policy adapter — see docs/extending.md.
type exampleGrants struct{}

func (exampleGrants) Name() string      { return "example" }
func (exampleGrants) Actions() []string { return []string{"job.run", "change.propose"} }
func (exampleGrants) RoleActions() map[mantlekeep.Role][]string {
	return map[mantlekeep.Role][]string{
		mantlekeep.RoleConsumer: {"job.run"},
		mantlekeep.RoleAIAgent:  {"change.propose"},
	}
}

func (exampleGrants) Admit(mantlekeep.PolicyIntent, []mantlekeep.Role) (bool, string) {
	return false, ""
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
