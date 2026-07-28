// Package identity provides IdentityResolver implementations. Mock (MVP) maps a
// fixed demo set; production resolves against AD/LDAP with AD group as the
// single source of truth.
package identity

import (
	"context"
	"fmt"

	mantlekeep "mantlekeep.dev/control"
)

// Mock is a fixed in-memory resolver for development and the Week-1 smoke test.
type Mock struct {
	subjects map[string]mantlekeep.Subject
}

// NewMock returns a resolver seeded with demo subjects across the role tiers.
func NewMock() *Mock {
	return &Mock{subjects: map[string]mantlekeep.Subject{
		"root":     {ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
		"arch-carol": {ID: "arch-carol", Roles: []mantlekeep.Role{mantlekeep.RoleArchitect}},
		"lead-bob":   {ID: "lead-bob", Roles: []mantlekeep.Role{mantlekeep.RoleOperator}},
		"dev-alice":  {ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
		"ci-agent":   {ID: "ci-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
		"ai-agent":   {ID: "ai-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
	}}
}

// Resolve implements mantlekeep.IdentityResolver. The Mock resolves by id and ignores
// any asserted groups — it is the dev directory.
func (m *Mock) Resolve(_ context.Context, ext mantlekeep.ExternalIdentity) (mantlekeep.Subject, error) {
	s, ok := m.subjects[ext.ID]
	if !ok {
		return mantlekeep.Subject{}, fmt.Errorf("unknown subject %q", ext.ID)
	}
	return s, nil
}
