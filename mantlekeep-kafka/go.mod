module github.com/mantlekeep/mantlekeep/mantlekeep-kafka

// Kept in lockstep with mantlekeep-control/go.mod — the CI toolchain is pinned to the
// patched release that clears the current stdlib advisories, and a module lagging behind
// would be scanned against an older stdlib.
go 1.25.0

require (
	github.com/mantlekeep/mantlekeep/mantlekeep-control v0.1.2
	github.com/twmb/franz-go v1.21.0
	github.com/twmb/franz-go/pkg/kadm v1.18.0
	github.com/twmb/franz-go/pkg/kmsg v1.13.1
)

require (
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	golang.org/x/crypto v0.52.0 // indirect
)

// Build against the core in this repository rather than the published tag, so a change
// to the ports and the adapter that follows it are proven together in one commit. A
// `replace` applies only to the main module, so a downstream consumer of this module
// still resolves the required version from the proxy — this cannot leak a local path
// into someone else's build.
replace github.com/mantlekeep/mantlekeep/mantlekeep-control => ../mantlekeep-control
