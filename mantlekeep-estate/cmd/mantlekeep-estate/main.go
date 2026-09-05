// Command mantlekeep-estate is the estate service with IN-MEMORY adapters.
//
// It exists so the governed flow can be driven end to end without a cluster or a database. The
// adapters forget everything on restart, which is why the service logs what it is running with
// rather than letting an operator discover it from behaviour.
//
// The composition root itself lives in github.com/mantlekeep/mantlekeep/mantlekeep-estate/serve. A binary chooses its adapters
// and calls Run — which is what keeps client-go and database drivers out of THIS module's
// dependency graph. The Kubernetes build is a different binary in a different module, beside
// the adapter it carries.
package main

import (
	"fmt"
	"os"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/memory"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/serve"
)

func main() {
	if err := serve.Run(serve.Options{
		Name: "mantlekeep-estate (in-memory adapters)",
		Ports: []estate.Port{
			memory.New("app", ""),
			memory.New("kafka", ""),
			memory.New("postgres", ""),
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
