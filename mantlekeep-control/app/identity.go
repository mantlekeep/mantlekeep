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

// DevSubjectsEnv names the dev directory's population, as "id=Role,Role;id2=Role" —
// e.g. "root=L0-SuperAdmin;dev-alice=L3-Consumer;ci-agent=AI-Agent".
//
// It exists so a deployment can look at itself with its OWN people in it rather than the
// six names compiled into this binary. It is dev-tier only and it is an ASSERTION: these
// identities were configured, not looked up, and every surface that shows them says so
// (see [mantlekeep.DirectoryDescriber]). Presenting a configured list as an authoritative
// directory would be a lie about the one screen people check before granting access.
//
// Ignored when MANTLEKEEP_AUTH=proxy, where a real gateway asserts identities instead.
const DevSubjectsEnv = "MANTLEKEEP_DEV_SUBJECTS"

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
	if configured := os.Getenv(DevSubjectsEnv); configured != "" {
		table := parseGroupRoles(configured)
		if len(table) == 0 {
			// Loud, not lenient. Falling back to the seeded demo set here would leave a
			// deployment answering about six people nobody configured, under a variable
			// the operator believes they set — and the first place that shows up is the
			// screen somebody reads before granting access.
			must(fmt.Errorf("%s is set but names no subject (want \"id=Role,Role;id2=Role\", "+
				"e.g. \"root=L0-SuperAdmin;dev-alice=L3-Consumer\")", DevSubjectsEnv))
		}
		fmt.Printf("identity: dev directory from %s (%d subjects, ASSERTED not looked up)\n",
			DevSubjectsEnv, len(table))
		return identity.NewMockFrom(table)
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

// Subjects implements [mantlekeep.SubjectLister] for the chain — and refuses unless EVERY
// resolver in it can be listed.
//
// A chain is a layered directory: a service rolemap in front of the human one. Listing the
// half that can be listed would produce a page that looks complete and is missing exactly
// the population the other resolver holds — every human, when the service map is the one
// that answered. A short list nobody can tell is short is worse than no list, so this
// returns the error and the surface says the directory cannot be listed.
func (c chainResolver) Subjects(ctx context.Context) ([]mantlekeep.Subject, error) {
	var all []mantlekeep.Subject
	for position, resolver := range c {
		lister, ok := resolver.(mantlekeep.SubjectLister)
		if !ok {
			return nil, fmt.Errorf("this deployment resolves identities through %d layered "+
				"directories and layer %d of them cannot be listed, so no complete list of "+
				"subjects exists here", len(c), position+1)
		}
		listed, err := lister.Subjects(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, listed...)
	}
	return all, nil
}

// DescribeDirectory implements [mantlekeep.DirectoryDescriber] for the chain. It is
// authoritative only if every layer is: one asserted layer makes the whole answer asserted.
func (c chainResolver) DescribeDirectory() mantlekeep.DirectoryDescription {
	described := mantlekeep.DirectoryDescription{Authoritative: true}
	for _, resolver := range c {
		describer, ok := resolver.(mantlekeep.DirectoryDescriber)
		if !ok {
			return mantlekeep.DirectoryDescription{
				Name: "several layered resolvers, at least one of which cannot say what it is",
				Note: "One layer of this deployment's identity chain does not describe itself, " +
					"so nothing here can tell you whether the roles below came from a directory " +
					"of record.",
			}
		}
		layer := describer.DescribeDirectory()
		if described.Name != "" {
			described.Name += ", layered in front of "
		}
		described.Name += layer.Name
		if layer.Note != "" {
			described.Note = strings.TrimSpace(described.Note + " " + layer.Note)
		}
		described.Authoritative = described.Authoritative && layer.Authoritative
	}
	return described
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
