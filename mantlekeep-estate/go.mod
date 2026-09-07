module github.com/mantlekeep/mantlekeep/mantlekeep-estate

// 1.25.0 is the true minimum: the module's own source needs nothing later, and the floor
// comes from the core's dependencies rather than from anything here. The directive is a
// HARD minimum for every consumer, not a record of what compiled it.
go 1.25.0

require (
	github.com/mantlekeep/mantlekeep/mantlekeep-control v0.2.0
	sigs.k8s.io/yaml v1.6.0
)

require go.yaml.in/yaml/v2 v2.4.2 // indirect

// No replace. The estate depends on the PUBLISHED core, which is the only arrangement a
// consumer can reproduce: a replace applies to the main module alone, so a module that
// needs one builds here and nowhere else. Verified by building this module with the
// replace removed, against the tag, before it was cut.

// The estate is built BOTH ways: as a published module a consumer imports, and as one module
// inside a clone of this repository. Without this, the second case silently downloads a
// DIFFERENT mantlekeep-control than the one sitting beside it — so a bank that clones the repo,
// scans it, and then builds this module in its own pipeline would build against code the scan
// never saw. `replace` applies only to the main module, so it governs our builds and is
// invisible to anyone importing the estate.
replace github.com/mantlekeep/mantlekeep/mantlekeep-control => ../mantlekeep-control
