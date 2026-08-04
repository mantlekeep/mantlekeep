package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/identity"
)

// BuildIdentity selects the identity resolver to pair with the authenticator, and —
// when a machine transport is on — LAYERS a service-account rolemap in front of it.
//
// The machine authenticators (mesh/mtls) place the SPIFFE/cert id in Principal.Groups,
// so the SAME groups→roles resolver that maps a human's IdP group→role also maps a
// SVID→role. MANTLEKEEP_SERVICE_ROLES supplies that table (least-privilege service tiers),
// e.g. "spiffe://cluster.local/ns/demo/sa/svc-a=L3-Consumer". Roles are still
// decided SERVER-SIDE from a VERIFIED id — a service never asserts its own role.
func BuildIdentity() mantlekeep.IdentityResolver {
	human := buildHumanIdentity()

	switch os.Getenv("MANTLEKEEP_TRANSPORT") {
	case "mesh", "native":
		svc := identity.NewGateway(parseGroupRoles(os.Getenv("MANTLEKEEP_SERVICE_ROLES")))
		// Try the service rolemap first (SVID→role); a browser principal has no SVID
		// entry, so it falls through to the human resolver.
		return chainResolver{svc, human}
	}
	return human
}

// buildHumanIdentity selects the human identity resolver. The dev tier resolves by id
// (the Mock directory); the proxy tier maps the SSO gateway's verified groups to roles
// via MantleKeep's own config table — so roles are still decided server-side, from groups
// the gateway cryptographically asserted.
func buildHumanIdentity() mantlekeep.IdentityResolver {
	if os.Getenv("MANTLEKEEP_AUTH") == "proxy" {
		table := parseGroupRoles(os.Getenv("MANTLEKEEP_GROUP_ROLES"))
		if len(table) == 0 {
			must(fmt.Errorf("MANTLEKEEP_AUTH=proxy needs MANTLEKEEP_GROUP_ROLES (e.g. \"platform-architects=L1-Architect;ops-operators=L2-Operator\")"))
		}
		var aiGroups []string
		if s := os.Getenv("MANTLEKEEP_AI_GROUPS"); s != "" {
			aiGroups = splitList(s)
		}
		fmt.Printf("identity: gateway groups→roles (%d group mappings)\n", len(table))
		return identity.NewGateway(table, aiGroups...)
	}
	return identity.NewMock()
}

// chainResolver tries each resolver in order and returns the first that resolves a
// Subject (non-empty roles, no error). It lets a machine transport layer a service
// rolemap in front of the human directory without either knowing about the other —
// the same first-wins shape as auth.Chain, one layer up.
type chainResolver []mantlekeep.IdentityResolver

func (c chainResolver) Resolve(ctx context.Context, ext mantlekeep.ExternalIdentity) (mantlekeep.Subject, error) {
	var lastErr error = fmt.Errorf("no resolver produced a subject for %q", ext.ID)
	for _, r := range c {
		s, err := r.Resolve(ctx, ext)
		if err == nil && len(s.Roles) > 0 {
			return s, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return mantlekeep.Subject{}, lastErr
}

// parseGroupRoles reads "group=Role,Role;group2=Role" into a group→roles table.
func parseGroupRoles(s string) map[string][]mantlekeep.Role {
	out := map[string][]mantlekeep.Role{}
	for _, pair := range strings.Split(s, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		var roles []mantlekeep.Role
		for _, r := range splitList(kv[1]) {
			roles = append(roles, mantlekeep.Role(r))
		}
		out[strings.TrimSpace(kv[0])] = roles
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
