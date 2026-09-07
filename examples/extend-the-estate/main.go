// Command example-estate is a WORKED EXAMPLE of extending MantleKeep without forking it.
//
// An organisation deploying MantleKeep normally wants three things the framework does not
// ship, and can have all three without changing a line of it:
//
//  1. its own NAME on the binary and its own environment prefix
//  2. its own ADAPTERS, because it knows what its infrastructure is and MantleKeep does not
//
// A third — its own IDENTITY source — is served by serve.Options.Callers in the estate's
// current source. It is not in the v0.1.0 tag this example pins, so it is not shown here.
// Until that is released, identity comes from the framework's trusted-header tier, which is
// fenced to loopback unless a deployment explicitly names the decision.
//
// Everything below is ordinary consumer code. It imports the published modules, implements
// published interfaces, and compiles against a version pinned in go.mod. Upgrading the
// framework is a version bump; nothing here is a copy of anything upstream, so nothing here
// can drift from it.
//
// Run it:
//
//	ACME_ESTATE_ADDR=127.0.0.1:8099 go run .
package main

import (
	"context"
	"fmt"
	"log"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/app"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/serve"
)

func main() {
	// 1. THE NAME AND THE ENVIRONMENT PREFIX.
	//
	// Operators here speak ACME_*; the framework still reads its own prefix underneath. The
	// remap fills only what the operator left empty, so setting the framework's own variable
	// directly still wins — which is what you want at three in the morning.
	app.Brand(app.BrandOptions{
		Prefix:  "ACME",
		Name:    "Acme Control",
		Mark:    "▲",
		Kicker:  "governed delivery",
		Tagline: "every action through one door",
	})

	brand := app.CurrentBrand()
	fmt.Printf("%s %s — %s\n", brand.Mark, brand.Name, brand.Tagline)
	fmt.Println("engine: the published framework, unmodified and pinned in go.sum")

	// 2. THE ADAPTERS.
	//
	// The framework knows the PORT; this binary knows the backend. Note what is NOT here:
	// no registration, no plugin discovery, no configuration naming a Go type. The binary
	// that knows both is the one that wires them, which is the only place that can.
	if err := serve.Run(serve.Options{
		Name:  "example-estate",
		Ports: []estate.Port{estate.Guarded(&deploymentAdapter{})},
	}); err != nil {
		log.Fatal(err)
	}
}

// deploymentAdapter is this organisation's adapter for one asset.
//
// It implements [estate.Approved] rather than [estate.Port], and is wrapped by
// [estate.Guarded] above. That is deliberate and it is the reason to prefer Approved: Guarded
// runs the refusals every adapter owes — an empty token, an expired token, a change for
// another asset, a kind this adapter does not handle — before the backend is touched. Four
// correct copies of those checks in four adapters is four places to forget the fifth, and
// they are not convenience code: they are what makes "govern before execute" true at the edge.
type deploymentAdapter struct{}

// Asset names what this adapter provisions. The framework never learns what it means.
func (a *deploymentAdapter) Asset() string { return "app" }

// Kinds lists the change kinds it handles within that asset. Empty would mean all of them.
//
// Declaring a kind is what lets two adapters share one asset: compose them with
// [estate.ByKind] and each takes the kinds it claims. Two adapters claiming one kind is
// refused when they are wired, not when a change arrives — an ambiguity resolved by map
// iteration order is the same change reaching a different backend on different days.
func (a *deploymentAdapter) Kinds() []string { return []string{"deployment"} }

// Observe reports what is REALLY there, which is the whole basis of drift detection.
//
// Report reality, never the intent. An adapter that echoes back what it was told to do makes
// every drift report say "no drift", which is worse than having no report at all.
func (a *deploymentAdapter) Observe(_ context.Context, team string) (estate.Observed, error) {
	// A real adapter reads its backend here.
	return estate.Observed{}, nil
}

// ApplyApproved makes one change, under a token the door already issued.
//
// Everything Guarded refuses has been refused before this runs. Two things about the token,
// and confusing them has cost people a security incident: token.Value is the opaque signed
// CAPABILITY it authorises with, and token.IntentID is the chain reference to RECORD on
// whatever it creates. Record the IntentID; never write the Value onto an object, into a
// label, or into a log line.
func (a *deploymentAdapter) ApplyApproved(
	_ context.Context, token mantlekeep.ExecutionToken, change estate.DesiredItem,
) error {
	fmt.Printf("  applying %s/%s under intent %s\n", change.Asset, change.Name, token.IntentID)
	return nil
}
