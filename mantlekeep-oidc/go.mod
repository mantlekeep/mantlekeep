module github.com/mantlekeep/mantlekeep/mantlekeep-oidc

// 1.25.0, matching the core: the directive is a hard minimum for every consumer, not a
// record of what compiled it.
go 1.25.0

require (
	github.com/mantlekeep/mantlekeep/mantlekeep-control v0.1.3
	github.com/mantlekeep/mantlekeep/mantlekeep-estate v0.1.0
)

// No replace: both dependencies are published, so this module resolves the same way for a
// consumer as it does here. A replace applies only to the main module — one that needs it
// builds here and nowhere else.
