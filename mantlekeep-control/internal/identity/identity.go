// Package identity provides IdentityResolver implementations. Mock (MVP) maps a
// fixed demo set; production resolves against AD/LDAP with AD group as the
// single source of truth.
package identity

import (
	"context"
	"fmt"
	"sort"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Mock is an in-memory resolver for development and the Week-1 smoke test.
//
// It is the one resolver in this tree that CAN be enumerated, because its whole population
// is a map somebody wrote down. That makes it useful to a screen showing who holds which
// role — and dangerous to the same screen, which is why it declares itself NOT
// authoritative: everything in here was asserted, by a fixture or by configuration, and
// nothing in here came from a directory of record.
type Mock struct {
	subjects map[string]mantlekeep.Subject
	// origin is the phrase a surface prints for where these people came from. The seeded
	// demo set and an operator's configured population are both fixtures, and both are worth
	// naming precisely — a reader who cannot see which one they are looking at cannot tell
	// a stale demo from their own configuration.
	origin string
}

var (
	_ mantlekeep.IdentityResolver   = (*Mock)(nil)
	_ mantlekeep.SubjectLister      = (*Mock)(nil)
	_ mantlekeep.DirectoryDescriber = (*Mock)(nil)
)

// NewMock returns a resolver seeded with demo subjects across the role tiers.
func NewMock() *Mock {
	return &Mock{
		origin: "the built-in dev directory (a fixed demo set compiled into this binary)",
		subjects: map[string]mantlekeep.Subject{
			"root":       {ID: "root", Roles: []mantlekeep.Role{mantlekeep.RoleSuperAdmin}},
			"arch-carol": {ID: "arch-carol", Roles: []mantlekeep.Role{mantlekeep.RoleArchitect}},
			"lead-bob":   {ID: "lead-bob", Roles: []mantlekeep.Role{mantlekeep.RoleOperator}},
			"dev-alice":  {ID: "dev-alice", Roles: []mantlekeep.Role{mantlekeep.RoleConsumer}},
			"ci-agent":   {ID: "ci-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
			"ai-agent":   {ID: "ai-agent", Roles: []mantlekeep.Role{mantlekeep.RoleAIAgent}, IsAI: true},
		}}
}

// NewMockFrom builds the dev directory from an id→roles table a deployment supplied,
// instead of the seeded demo set. See app.DevSubjectsEnv for how it is configured.
//
// A subject holding [mantlekeep.RoleAIAgent] is flagged IsAI, exactly as the gateway resolver
// does for a group that maps to that role — the AI guardrails must arm the same way
// whichever resolver produced the subject, or a service account can be de-fanged by being
// declared in the other one.
//
// An entry with no roles is KEPT, with none. Dropping it would make somebody who was
// configured wrongly look like somebody who was never configured at all, and those two
// send a reader to different places.
func NewMockFrom(table map[string][]mantlekeep.Role) *Mock {
	subjects := make(map[string]mantlekeep.Subject, len(table))
	for id, roles := range table {
		subject := mantlekeep.Subject{ID: id, Roles: roles}
		for _, role := range roles {
			if role == mantlekeep.RoleAIAgent {
				subject.IsAI = true
			}
		}
		subjects[id] = subject
	}
	return &Mock{
		origin:   "a dev directory asserted by this deployment's configuration",
		subjects: subjects,
	}
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

// Subjects implements [mantlekeep.SubjectLister]: the whole population, sorted by id.
//
// Sorted so the same directory renders the same page twice running — an ordering that
// wanders makes two screenshots differ for no reason anybody can act on.
func (m *Mock) Subjects(context.Context) ([]mantlekeep.Subject, error) {
	out := make([]mantlekeep.Subject, 0, len(m.subjects))
	for _, subject := range m.subjects {
		out = append(out, subject)
	}
	sort.Slice(out, func(first, second int) bool { return out[first].ID < out[second].ID })
	return out, nil
}

// DescribeDirectory implements [mantlekeep.DirectoryDescriber]. Never authoritative: every
// subject here was asserted by a fixture or by configuration.
func (m *Mock) DescribeDirectory() mantlekeep.DirectoryDescription {
	return mantlekeep.DirectoryDescription{
		Name:          m.origin,
		Authoritative: false,
		Note: "These identities are ASSERTED, not looked up: nothing here was read from a " +
			"directory of record, so this is who this deployment was told about and not who " +
			"your organisation employs. Do not read it as a list of who has access.",
	}
}
