package identity

import (
	"context"
	"fmt"

	mantlekeep "mantlekeep.dev/control"
)

// Gateway resolves a claims-based identity (asserted by an SSO gateway and
// already VERIFIED by the ProxyAuthenticator) to a Subject by mapping the IdP
// groups to MantleKeep roles. The group→role table is MantleKeep's own config, so
// authorization is decided SERVER-SIDE from verified groups — the gateway proves
// who you are; MantleKeep decides what that means. This is the host-tier resolver.
type Gateway struct {
	table    map[string][]mantlekeep.Role // IdP group → MantleKeep roles
	aiGroups map[string]bool          // groups that mark a subject as an AI agent
}

// NewGateway builds the resolver from a group→roles table. Groups in aiGroups (or
// any group mapping to AI-Agent) flag the subject as AI, arming the AI guardrails.
func NewGateway(table map[string][]mantlekeep.Role, aiGroups ...string) *Gateway {
	ai := make(map[string]bool, len(aiGroups))
	for _, g := range aiGroups {
		ai[g] = true
	}
	return &Gateway{table: table, aiGroups: ai}
}

// Resolve maps the verified groups to the union of granted roles.
func (g *Gateway) Resolve(_ context.Context, ext mantlekeep.ExternalIdentity) (mantlekeep.Subject, error) {
	seen := map[mantlekeep.Role]bool{}
	var roles []mantlekeep.Role
	isAI := false
	for _, grp := range ext.Groups {
		if g.aiGroups[grp] {
			isAI = true
		}
		for _, r := range g.table[grp] {
			if r == mantlekeep.RoleAIAgent {
				isAI = true
			}
			if !seen[r] {
				seen[r] = true
				roles = append(roles, r)
			}
		}
	}
	if len(roles) == 0 {
		return mantlekeep.Subject{}, fmt.Errorf("identity %q has no MantleKeep role for its groups %v", ext.ID, ext.Groups)
	}
	return mantlekeep.Subject{ID: ext.ID, Roles: roles, ADGroups: ext.Groups, IsAI: isAI}, nil
}
