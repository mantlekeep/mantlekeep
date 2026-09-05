module github.com/mantlekeep/mantlekeep/mantlekeep-estate

// 1.25.0 is the true minimum: the module's own source needs nothing later, and the floor
// comes from the core's dependencies rather than from anything here. The directive is a
// HARD minimum for every consumer, not a record of what compiled it.
go 1.25.0

require (
	github.com/mantlekeep/mantlekeep/mantlekeep-control v0.1.2
	sigs.k8s.io/yaml v1.6.0
)

require go.yaml.in/yaml/v2 v2.4.2 // indirect

// Build against the core in this repository rather than the published tag, so a change to
// the ports and the adapters that follow it are proven together in one commit. A replace
// applies only to the main module, so a downstream consumer still resolves the required
// version from the proxy — this cannot leak a local path into someone else's build.
replace github.com/mantlekeep/mantlekeep/mantlekeep-control => ../mantlekeep-control
