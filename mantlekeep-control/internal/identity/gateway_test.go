package identity

import (
	"context"
	"testing"

	mantlekeep "mantlekeep.dev/control"
)

func TestGatewayMapsGroupsToRoles(t *testing.T) {
	g := NewGateway(map[string][]mantlekeep.Role{
		"platform-architects": {mantlekeep.RoleArchitect},
		"ops-operators":  {mantlekeep.RoleOperator},
		"ai-agents":           {mantlekeep.RoleAIAgent},
	}, "ai-agents")

	// Verified groups → the union of mapped roles, id preserved, groups recorded.
	s, err := g.Resolve(context.Background(), mantlekeep.ExternalIdentity{
		ID: "carol@example.com", Groups: []string{"platform-architects", "ops-operators"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "carol@example.com" || len(s.Roles) != 2 {
		t.Fatalf("bad subject: %+v", s)
	}
	if s.IsAI {
		t.Fatal("human wrongly flagged AI")
	}

	// An AI-agent group flags IsAI (arms the AI guardrails).
	ai, _ := g.Resolve(context.Background(), mantlekeep.ExternalIdentity{ID: "bot", Groups: []string{"ai-agents"}})
	if !ai.IsAI || len(ai.Roles) != 1 || ai.Roles[0] != mantlekeep.RoleAIAgent {
		t.Fatalf("ai subject not resolved: %+v", ai)
	}

	// No mapped group → rejected (no default roles).
	if _, err := g.Resolve(context.Background(), mantlekeep.ExternalIdentity{ID: "x", Groups: []string{"unknown"}}); err == nil {
		t.Fatal("identity with no mapped role should be rejected")
	}
}
