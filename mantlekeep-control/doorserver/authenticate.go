package doorserver

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Authenticator establishes WHO is calling, by checking a credential rather than reading a
// header somebody upstream was supposed to have set.
//
// # Why the door needs this more than anything in front of it
//
// The door is where decisions are made and where the chain is written. Everything downstream
// inherits its answer: a service that verifies its own callers still submits to the door, so
// a caller who reaches the door directly skips that verification entirely.
//
// And the door's delegation model raises the stakes. Trusting a header for IDENTITY lets a
// caller be one person. Trusting it for DELEGATOR status lets them be the service that speaks
// for everyone — requester and approver at once, with the chain recording both as fact.
//
// # People and services are both callers
//
// An Authenticator returns an id, and the door does not care whether that id belongs to a
// person or a service. A person arrives with an OIDC token; a service arrives with its own
// workload credential. What matters is that the id was PROVEN rather than asserted, because
// [Server.isDelegator] then checks a verified name against the permitted list.
type Authenticator interface {
	// Authenticate returns the caller's id, or an error when the credential is missing,
	// malformed or does not verify.
	//
	// An error is a refusal, never an anonymous caller. A door that fell back to "unknown"
	// would record decisions against a subject that is not a subject.
	Authenticate(request *http.Request) (string, error)
}

// TrustHeaderVar allows the trusted-header tier on an address other people can reach.
//
// It exists because a deployment whose gateway genuinely is the only network path is a real
// deployment, and refusing it outright would be wrong. But it must be a decision somebody
// made and can be asked about, rather than the behaviour nobody noticed.
const TrustHeaderVar = "MANTLEKEEP_TRUST_HEADER"

// checkIdentityTier refuses a configuration that trusts the transport on an address the
// transport does not protect, and says so out loud when it allows one.
//
// Running with no identity provider is legitimate and necessary — a framework that cannot be
// started in five minutes does not get tried, and the header and dev-login tiers are what
// make a laptop, a demo and an air-gapped first day possible. What is not legitimate is
// drifting into production that way without anyone deciding to.
func checkIdentityTier(options Options, addr string, trustHeaderChosen bool) error {
	if options.Authenticator != nil {
		slog.Info("door identity is VERIFIED — a credential is checked on every request")
		return nil
	}

	tiers := make([]string, 0, 2)
	if options.TrustedUserHeader != "" {
		tiers = append(tiers, "the "+options.TrustedUserHeader+" header")
	}
	if options.DevLogin {
		tiers = append(tiers, "dev-login, which mints a session for any named user with NO "+
			"credential check")
	}
	if len(tiers) == 0 {
		return nil
	}

	if loopbackOnly(addr) {
		slog.Warn("door identity is TAKEN ON TRUST, not verified — every decision on the "+
			"chain will be attributed to whoever supplied it. Allowed here only because the "+
			"listen address is loopback",
			"trusting", strings.Join(tiers, " and "), "addr", addr)
		return nil
	}
	if !trustHeaderChosen {
		// Refused, not warned. The door's delegation list is the whole control on
		// impersonation, and it checks a name this tier does not establish — so off loopback
		// anything that can route to this port can name itself a delegator and then name any
		// subject, and the chain records it as fact.
		return fmt.Errorf(
			"door: refusing to take identity on trust on %s — %s establishes nothing an "+
				"attacker cannot also supply, and the delegation list checks a name it does "+
				"not prove. Configure an Authenticator, or set %s=true if a gateway is "+
				"genuinely the only path to this address",
			addr, strings.Join(tiers, " and "), TrustHeaderVar)
	}
	slog.Warn("door identity is TAKEN ON TRUST on a non-loopback address because "+
		TrustHeaderVar+"=true. Whatever fronts this port is now the only thing preventing "+
		"impersonation, including impersonation of a DELEGATOR",
		"trusting", strings.Join(tiers, " and "), "addr", addr)
	return nil
}

// loopbackOnly reports whether this address is reachable only from this machine.
//
// An empty host means every interface, which is the case that matters: ":8080" looks local in
// a terminal and is reachable from the network.
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
