// Command acme-govern is the WHITE-LABEL EXAMPLE: how to ship your own branded
// binary on top of MantleKeep without forking, renaming, or copying a line of the
// engine.
//
// "Acme" here is a stand-in for your organisation. Copy this directory, change the
// brandPrefix constant and the four brand defaults, and you have your own build —
// the core never moves.
//
// It links NO extra engine code. It builds the SAME door, the same pure-Go RBAC
// policy, and the same hash-chained audit as the stock `mantlekeep` binary. Only
// two things differ, and both are configuration:
//
//  1. It remaps the ACME_* environment onto the core's MANTLEKEEP_* names, so an
//     operator sets ACME_POLICY_DIR / ACME_BRAND_NAME / … and never types a
//     MANTLEKEEP_ name. One call covers every variable, present and future,
//     because it works by prefix rather than an enumerated list.
//  2. It sets its own brand defaults, still overridable from the environment.
//
// This is the seam that makes white-labelling safe: because the engine stays an
// unmodified dependency, a MantleKeep security fix reaches this binary by bumping
// a version. A fork would have to re-apply that fix by hand, forever — which is
// why renaming the framework's packages is the one thing you must not do.
//
// Run:  go build -o acme-govern ./cmd/acme-govern && ACME_BRAND_NAME="Acme Control" ./acme-govern
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	mantlekeep "mantlekeep.dev/control"
	"mantlekeep.dev/control/app"
	"mantlekeep.dev/control/doorkit"
)

// brandPrefix is the ONLY namespace your operators ever type. Change this one
// constant (plus the four defaults below) and the binary is yours.
const brandPrefix = "ACME"

func main() {
	// ONE call makes this binary itself. Note what is NOT here: no MANTLEKEEP_
	// variable names. The framework owns its own namespace, so a brand never has to
	// know it — operators speak ACME_*, and any ACME_BRAND_* they set wins over these
	// defaults. Change these five lines and the binary is yours.
	app.Brand(app.BrandOptions{
		Prefix:  brandPrefix,
		Name:    "Acme Control",
		Mark:    "◆",
		Kicker:  "governed delivery",
		Tagline: "every action through one door",
	})

	brand := app.CurrentBrand()
	fmt.Printf("%s %s — %s\n", brand.Mark, brand.Name, brand.Tagline)
	fmt.Println("────────────────────────────────────────────────────────")
	fmt.Printf("(engine: unmodified MantleKeep core; brand prefix %s_*)\n\n", brandPrefix)

	// 3. The SAME door as the stock binary — assembled through the public seam.
	//    Nothing here is branded: the brand is the face, never the engine.
	ctx := context.Background()
	auditPath := filepath.Join(os.TempDir(), "acme-govern-audit.db")
	_ = os.Remove(auditPath)

	door, err := doorkit.NewInMemoryDoor(auditPath)
	must(err)
	defer func() { _ = door.Audit.(interface{ Close() error }).Close() }()

	// An allowed action: the engine-baked superadmin wildcard.
	token, err := door.Submitter.Submit(ctx, mantlekeep.Intent{
		ID: "ACME-001", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		Spec:    mantlekeep.IntentSpec{Goal: "prove the branded binary uses the real door"},
	})
	must(err)
	fmt.Printf("  ALLOW  job.run            → execution token %s…\n", token.Value[:8])

	// A denied action: no goal declared. The floor is in the ENGINE, so a brand
	// cannot configure its way past it — that is the point of a sealed floor.
	_, err = door.Submitter.Submit(ctx, mantlekeep.Intent{
		ID: "ACME-002", Action: "job.run", Resource: "project/demo",
		Subject: mantlekeep.Subject{ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
	})
	if err == nil {
		fmt.Println("  UNEXPECTED: an intent with no goal was allowed")
		os.Exit(1)
	}
	fmt.Printf("  DENY   job.run (no goal)  → %v\n", err)

	// The audit chain is the engine's, not the brand's: both records are on it.
	ok, err := door.Audit.Verify(ctx)
	must(err)
	fmt.Printf("\n  audit chain intact: %v\n", ok)
	fmt.Println("\nSame engine, different face. Nothing was forked to get here.")
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
