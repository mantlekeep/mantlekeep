package serve

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
)

// Environment that chooses how a caller is identified.
const (
	// TrustHeaderVar allows the trusted-header resolver on an address other people can
	// reach. It exists because refusing outright would be wrong for a deployment whose
	// gateway genuinely is the only path — but it must be a decision somebody made and can
	// be asked about, not a default nobody noticed.
	TrustHeaderVar = "MANTLEKEEP_TRUST_HEADER"
)

// resolveCallers chooses how this deployment identifies a caller, and says so out loud.
//
// # Why the choice is loud
//
// Running with no identity provider is a legitimate and necessary thing: a framework that
// cannot be started in five minutes does not get tried, and the trusted-header resolver is
// what makes a laptop, a demo and an air-gapped first day possible.
//
// What is NOT legitimate is drifting into production that way without anyone deciding to.
// So the trusted-header path is fenced exactly as the portal's development identity is: it
// works freely on loopback, and off loopback it must be chosen by name.
//
// The estate already warns when it is read-only and when cluster reachability is assumed.
// Identity deserves it more than either: it is the field every record on the chain is keyed
// on, and a chain of decisions attributed to whoever typed a header is evidence of nothing.
func resolveCallers(addr string, supplied api.CallerResolver) (api.CallerResolver, error) {
	if supplied != nil {
		slog.Info("identity is VERIFIED by the resolver this binary supplied")
		return supplied, nil
	}

	// Nothing supplied — the trusted-header tier.
	if loopbackOnly(addr) {
		slog.Warn("identity is TAKEN FROM A HEADER, not verified — every decision on the "+
			"chain will be attributed to whoever set it. This is the development tier and is "+
			"allowed here only because the listen address is loopback",
			"header", api.UserHeader, "addr", addr,
			"to verify instead", "supply Options.Callers from mantlekeep-oidc")
		return api.HeaderCallers{}, nil
	}

	if os.Getenv(TrustHeaderVar) != "true" {
		// Refused rather than warned. Off loopback, an unverified header is an open door to
		// anything that can route to this port — and the failure is silent, because a forged
		// identity produces a perfectly ordinary-looking record.
		return nil, fmt.Errorf(
			"identity: refusing to trust the %s header on %s — anything that can reach this "+
				"port could then be anyone, and the chain would record it as fact. Supply "+
				"a verifying resolver in Options.Callers, or set %s=true if a gateway is genuinely "+
				"the only path to this address",
			api.UserHeader, addr, TrustHeaderVar)
	}

	slog.Warn("identity is TAKEN FROM A HEADER on a non-loopback address, because "+
		TrustHeaderVar+"=true. Everything reaching this port is believed about who it is; "+
		"the gateway in front is now the only thing preventing impersonation",
		"header", api.UserHeader, "addr", addr)
	return api.HeaderCallers{}, nil
}

// loopbackOnly reports whether this address is reachable only from this machine.
//
// An empty host means every interface, which is the case that matters: ":8092" looks local
// in a terminal and is reachable from the network.
func loopbackOnly(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}
