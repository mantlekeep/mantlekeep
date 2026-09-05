module github.com/mantlekeep/mantlekeep/mantlekeep-oidc

// 1.25.0, matching the core: the directive is a hard minimum for every consumer, not a
// record of what compiled it.
go 1.25.0

require (
	github.com/mantlekeep/mantlekeep/mantlekeep-control v0.1.3
	github.com/mantlekeep/mantlekeep/mantlekeep-estate v0.1.0
)

replace github.com/mantlekeep/mantlekeep/mantlekeep-control => ../mantlekeep-control

replace github.com/mantlekeep/mantlekeep/mantlekeep-estate => ../mantlekeep-estate
