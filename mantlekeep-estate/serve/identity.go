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
	// IssuerVar, AudienceVar and JWKSVar configure token VERIFICATION. Set them and the
	// estate stops trusting the transport and checks for itself.
	IssuerVar   = "MANTLEKEEP_OIDC_ISSUER"
	AudienceVar = "MANTLEKEEP_OIDC_AUDIENCE"
	JWKSVar     = "MANTLEKEEP_OIDC_JWKS_FILE"

	// SubjectClaimVar names the claim carrying the caller. Defaults to "sub"; deployments
	// keyed on email or preferred_username set it here.
	SubjectClaimVar = "MANTLEKEEP_OIDC_SUBJECT_CLAIM"

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
func resolveCallers(addr string) (api.CallerResolver, error) {
	issuer := strings.TrimSpace(os.Getenv(IssuerVar))
	audience := strings.TrimSpace(os.Getenv(AudienceVar))
	jwksPath := strings.TrimSpace(os.Getenv(JWKSVar))

	if issuer != "" || audience != "" || jwksPath != "" {
		return verifyingCallers(issuer, audience, jwksPath)
	}

	// No verification configured — the trusted-header tier.
	if loopbackOnly(addr) {
		slog.Warn("identity is TAKEN FROM A HEADER, not verified — every decision on the "+
			"chain will be attributed to whoever set it. This is the development tier and is "+
			"allowed here only because the listen address is loopback",
			"header", api.UserHeader, "addr", addr,
			"to verify instead", IssuerVar+" + "+AudienceVar+" + "+JWKSVar)
		return api.HeaderCallers{}, nil
	}

	if os.Getenv(TrustHeaderVar) != "true" {
		// Refused rather than warned. Off loopback, an unverified header is an open door to
		// anything that can route to this port — and the failure is silent, because a forged
		// identity produces a perfectly ordinary-looking record.
		return nil, fmt.Errorf(
			"identity: refusing to trust the %s header on %s — anything that can reach this "+
				"port could then be anyone, and the chain would record it as fact. Configure "+
				"verification (%s, %s, %s), or set %s=true if a gateway is genuinely the only "+
				"path to this address",
			api.UserHeader, addr, IssuerVar, AudienceVar, JWKSVar, TrustHeaderVar)
	}

	slog.Warn("identity is TAKEN FROM A HEADER on a non-loopback address, because "+
		TrustHeaderVar+"=true. Everything reaching this port is believed about who it is; "+
		"the gateway in front is now the only thing preventing impersonation",
		"header", api.UserHeader, "addr", addr)
	return api.HeaderCallers{}, nil
}

// verifyingCallers builds the token verifier, refusing a half-configured one.
func verifyingCallers(issuer, audience, jwksPath string) (api.CallerResolver, error) {
	// All three or none. A verifier missing its audience accepts tokens minted for any other
	// service by the same issuer, which is a subtler hole than no verification at all —
	// nobody reviews a deployment that appears to be verifying.
	for name, value := range map[string]string{
		IssuerVar: issuer, AudienceVar: audience, JWKSVar: jwksPath,
	} {
		if value == "" {
			return nil, fmt.Errorf(
				"identity: %s is not set, and a partly-configured verifier is worse than none "+
					"— it looks like verification while accepting tokens it should refuse", name)
		}
	}

	document, err := os.ReadFile(jwksPath) // #nosec G304,G703 -- an operator-supplied path
	if err != nil {
		return nil, fmt.Errorf("identity: reading the issuer's keys from %s: %w", jwksPath, err)
	}
	keys, err := api.NewStaticKeys(document)
	if err != nil {
		return nil, err
	}

	slog.Info("identity is VERIFIED from a bearer token",
		"issuer", issuer, "audience", audience, "keys", jwksPath)
	return &api.VerifiedCallers{
		Issuer: issuer, Audience: audience, Keys: keys,
		SubjectClaim: os.Getenv(SubjectClaimVar),
	}, nil
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
