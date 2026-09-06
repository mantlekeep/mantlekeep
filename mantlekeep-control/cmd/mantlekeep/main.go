// Command mantlekeep is the Week-1 smoke test of the one door — and the Tier-0
// proof: a single binary, no Docker, "just run it" (like v3 `java -jar`).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/audit"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/identity"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/sdk"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/orchestrator"
)

// The demo's fixed vocabulary. These are EXAMPLE values, not core concepts: the engine
// names no action and no resource of its own, so every one of them is a string this
// smoke test chose. Named once here so the cases below and the example provider that
// grants them can never drift apart — a case naming an action the provider does not
// grant would silently turn this demo's expected ALLOW into a DENY.
const (
	// The one resource every demo case acts on; its value is arbitrary, only its sameness matters.
	demoResource = "project/demo"

	// Granted to L3-Consumer by exampleGrants below.
	actionJobRun = "job.run"
	// Granted to AI-Agent below: an AI may PROPOSE a change.
	actionChangePropose = "change.propose"
	// Deliberately granted to NO ONE, so the demo shows a real deny: approval is the act
	// an AI may never perform, and the core ships zero grants of its own.
	actionChangeApprove = "change.approve"
)

// sectionDivider separates the demo's printed sections. Its width matches the banner
// lines it sits under.
const sectionDivider = "────────────────────────────────────────────────────────"

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

	// Unique 0700 dir (not a predictable shared-temp name) — avoids a symlink/pre-create attack on
	// the audit file. os.MkdirTemp creates it owner-only; we clean it up on exit.
	dir, err := os.MkdirTemp("", "mantlekeep-*")
	must(err)
	defer func() { _ = os.RemoveAll(dir) }()
	aud, err := audit.Open(filepath.Join(dir, "audit.db"))
	must(err)
	defer aud.Close()

	// Pure-Go default policy — no OPA linked into the core binary. The core ships ZERO grants
	// (the data wall): a deployable wires the policy it ships via WithProviders. Here a tiny
	// EXAMPLE provider supplies demo grants so this smoke test shows a real allow/deny mix —
	// swap it for your product's policy adapter (see docs/extending.md).
	door := sdk.New(identity.NewMock(), policy.NewRBAC().WithProviders(exampleGrants{}), aud)

	fmt.Println("🔱 MantleKeep — the one door (Week-1 smoke test)")
	fmt.Println(sectionDivider)

	cases := []mantlekeep.Intent{
		// SuperAdmin (L0) — the engine-baked wildcard → ALLOWED (needs no grant)
		{ID: "INT-001", Action: actionJobRun, Resource: demoResource,
			Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
			Spec:    mantlekeep.IntentSpec{Goal: "run anything — superadmin wildcard"}},
		// Consumer runs a job → ALLOWED (granted to L3-Consumer by the example provider)
		{ID: "INT-002", Action: actionJobRun, Resource: demoResource,
			Subject: mantlekeep.Subject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
			Spec:    mantlekeep.IntentSpec{Goal: "run the build"}},
		// AI agent proposes a change → ALLOWED (AI may propose; granted to AI-Agent)
		{ID: "INT-003", Action: actionChangePropose, Resource: demoResource,
			Subject: mantlekeep.Subject{ID: "ci-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
			Spec:    mantlekeep.IntentSpec{Goal: "propose a dependency bump"}},
		// Intent with NO goal → DENIED (declare-before-execute)
		{ID: "INT-004", Action: actionJobRun, Resource: demoResource,
			Subject: mantlekeep.Subject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
			Spec:    mantlekeep.IntentSpec{Goal: ""}},
		// AI agent tries to APPROVE → DENIED (never granted — the core ships zero grants; you supply them)
		{ID: "INT-005", Action: actionChangeApprove, Resource: demoResource,
			Subject: mantlekeep.Subject{ID: "ci-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
			Spec:    mantlekeep.IntentSpec{Goal: "approve the change"}},
	}

	for _, in := range cases {
		tok, err := door.Submit(ctx, in)
		if err != nil {
			fmt.Printf("  ✗ %-9s %-22s %-22s DENIED  — %v\n", in.Subject.ID, in.Action, in.ID, err)
			continue
		}
		// The INTENT id, never the token. Value is the capability the door issued; a
		// prefix of it is still a fragment of a live credential, and stdout in a container
		// is a log aggregator. The intent id identifies the same decision and can be looked
		// up on the chain, which the token cannot.
		fmt.Printf("  ✓ %-9s %-22s %-22s ALLOWED — intent %s\n",
			in.Subject.ID, in.Action, in.ID, tok.IntentID)
	}

	ok, err := aud.Verify(ctx)
	must(err)
	fmt.Println(sectionDivider)
	fmt.Printf("audit hash-chain intact: %v\n", ok)

	// ── The spine — run a governed DAG under a token ─────────────────────────
	// This shows the ENGINE (orchestrator + saga), never a product: a GENERIC two-step DAG
	// (step-a → step-b) driven by a tiny inline runner. Real product DAGs live in the product
	// catalog (mantlekeep.dev/portal), and real execution lives behind WorkerPort in a worker
	// deployable — the core carries neither. We mint the token as the SuperAdmin wildcard so the
	// demo needs zero external policy data (grants are DATA the core no longer embeds).
	fmt.Println()
	fmt.Println("🔱 MantleKeep — the spine (orchestrator engine + saga)")
	fmt.Println(sectionDivider)

	token, err := door.Submit(ctx, mantlekeep.Intent{
		ID: "INT-100", Action: "spine.run", Resource: "demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "run the generic spine demo"},
	})
	must(err)
	fmt.Printf("  door issued an execution token for intent %s\n", token.IntentID)

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
func (exampleGrants) Actions() []string { return []string{actionJobRun, actionChangePropose} }
func (exampleGrants) RoleActions() map[mantlekeep.Role][]string {
	return map[mantlekeep.Role][]string{
		mantlekeep.RoleConsumer: {actionJobRun},
		mantlekeep.RoleAIAgent:  {actionChangePropose},
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
