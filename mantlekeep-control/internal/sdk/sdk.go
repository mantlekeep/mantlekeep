// Package sdk implements the one door — mantlekeep.Submitter. Humans and AI both
// call Submit. It validates the intent, evaluates policy, records the decision
// in the audit trail, and issues a scoped ExecutionToken on allow. No bypass.
package sdk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	mantlekeep "mantlekeep.dev/control"
)

// SDK wires identity, policy, and audit into the single submission path.
type SDK struct {
	identity mantlekeep.IdentityResolver
	policy   mantlekeep.PolicyEvaluator
	audit    mantlekeep.AuditLogger
}

// New constructs the SDK from its collaborators.
func New(id mantlekeep.IdentityResolver, pol mantlekeep.PolicyEvaluator, aud mantlekeep.AuditLogger) *SDK {
	return &SDK{identity: id, policy: pol, audit: aud}
}

// Submit implements mantlekeep.Submitter.
func (s *SDK) Submit(ctx context.Context, intent mantlekeep.Intent) (mantlekeep.ExecutionToken, error) {
	// Belt-and-suspenders: the policy also enforces this, but reject early.
	if intent.Spec.Goal == "" {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("intent rejected: intent_spec.goal is required")
	}

	// Identity is resolved SERVER-SIDE from the verified id and (for the SSO tier)
	// the verified IdP groups — the resolver, using MantleKeep's own config, decides
	// the roles. A caller's claimed roles (Subject.Roles) are NEVER read here. This
	// is what stops role forgery: you can say who you are, but the door decides what
	// that identity is allowed to be. The groups themselves are trusted only because
	// the ProxyAuthenticator cryptographically verified the gateway's assertion.
	subject, err := s.identity.Resolve(ctx, mantlekeep.ExternalIdentity{
		ID:     intent.Subject.ID,
		Groups: intent.Subject.ADGroups,
	})
	if err != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("intent rejected: unknown subject %q: %w", intent.Subject.ID, err)
	}

	// A run approval carries the original requester (for separation of duties), and an env-gated
	// action carries a target environment. Both are read GENERICALLY off params — the engine names
	// neither the verb nor the environment; env-gating is a product's floor DATA (required_role_when).
	requester, _ := intent.Params["requester"].(string)
	env, _ := intent.Params["env"].(string)
	scope, _ := intent.Params["scope"].(string) // generic tenancy scope (SDLC's project) → selects the scope's policy tier

	input := mantlekeep.PolicyInput{
		Subject: mantlekeep.PolicySubject{
			ID:    subject.ID,
			Roles: subject.Roles,
			IsAI:  subject.IsAI,
			Attrs: subject.Attrs,
		},
		Intent: mantlekeep.PolicyIntent{
			Action:    intent.Action,
			Resource:  intent.Resource,
			Requester: requester,
			Env:       env,
			Goal:      intent.Spec.Goal,
			Scope:     scope,
			Params:    intent.Params,
		},
	}

	decision, err := s.policy.Evaluate(ctx, input)
	if err != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("policy error: %w", err)
	}

	// Evidence is a byproduct: every decision is recorded, allow or deny.
	if _, aerr := s.audit.Log(ctx, mantlekeep.AuditRecord{
		Timestamp: time.Now().UTC(),
		IntentID:  intent.ID,
		SubjectID: subject.ID,
		Action:    intent.Action,
		Decision:  decision.Action,
		PolicyID:  decision.PolicyID,
		IsAI:      subject.IsAI,
		Via:       intent.Via,
	}); aerr != nil {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("audit error: %w", aerr)
	}

	if decision.Action != mantlekeep.ActionAllow {
		return mantlekeep.ExecutionToken{}, fmt.Errorf("%s: %s", decision.Action, decision.Reason)
	}

	now := time.Now().UTC()
	return mantlekeep.ExecutionToken{
		Value:     randToken(),
		IntentID:  intent.ID,
		Scope:     intent.Resource,
		IssuedAt:  now,
		ExpiresAt: now.Add(2 * time.Hour),
	}, nil
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
