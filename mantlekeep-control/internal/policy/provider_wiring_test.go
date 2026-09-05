package policy

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// These pin what WithProviders BUILDS out of a provider: the action→provider index that
// routes an admission check, and the role→action grants folded into the core's generic
// role check. Both are consulted on every decision, so a wiring mistake here is a
// governance hole that no other test would notice.

type fakeProvider struct {
	name    string
	actions []string
	grants  map[mantlekeep.Role][]string
	deny    string // non-empty → Admit denies with this reason
}

func (p fakeProvider) Name() string                              { return p.name }
func (p fakeProvider) Actions() []string                         { return p.actions }
func (p fakeProvider) RoleActions() map[mantlekeep.Role][]string { return p.grants }
func (p fakeProvider) Admit(mantlekeep.PolicyIntent, []mantlekeep.Role) (bool, string) {
	if p.deny != "" {
		return true, p.deny
	}
	return false, ""
}

func TestWithProvidersIndexesActionsAndFoldsInRoleGrants(t *testing.T) {
	p := fakeProvider{
		name:    "sdlc",
		actions: []string{"sdlc.build", "sdlc.release"},
		grants:  map[mantlekeep.Role][]string{mantlekeep.RoleConsumer: {"sdlc.build"}},
	}
	r := NewRBAC().WithProviders(p)

	for _, action := range p.actions {
		if r.byAction[action] == nil {
			t.Errorf("action %q was not routed to its provider — its floor would never run", action)
		}
	}
	if !r.providerRoleActions[string(mantlekeep.RoleConsumer)]["sdlc.build"] {
		t.Error("the provider's role grant did not fold into the generic role check")
	}
	if r.providerRoleActions[string(mantlekeep.RoleConsumer)]["sdlc.release"] {
		t.Error("a grant the provider never made was folded in")
	}
}

// Several products are wired into one deployable; each must keep its own actions and
// grants, and a role granted actions by two providers must end up holding both.
func TestWithProvidersAccumulatesAcrossProvidersAndCalls(t *testing.T) {
	build := fakeProvider{
		name: "sdlc", actions: []string{"sdlc.build"},
		grants: map[mantlekeep.Role][]string{mantlekeep.RoleConsumer: {"sdlc.build"}},
	}
	deploy := fakeProvider{
		name: "estate", actions: []string{"estate.place"},
		grants: map[mantlekeep.Role][]string{mantlekeep.RoleConsumer: {"estate.place"}},
	}

	// One call with two providers, and a second chained call, must behave the same way.
	r := NewRBAC().WithProviders(build).WithProviders(deploy)

	consumer := r.providerRoleActions[string(mantlekeep.RoleConsumer)]
	if !consumer["sdlc.build"] || !consumer["estate.place"] {
		t.Errorf("a later provider dropped an earlier one's grants: %v", consumer)
	}
	if r.byAction["sdlc.build"] == nil || r.byAction["estate.place"] == nil {
		t.Error("a later provider dropped an earlier one's action routing")
	}
	if len(r.providers) != 2 {
		t.Errorf("registered %d providers, want 2", len(r.providers))
	}
}

// Nil providers are skipped rather than panicking — a deployable may wire an adapter
// conditionally and pass nil when it is not built in.
func TestWithProvidersSkipsNilProviders(t *testing.T) {
	real := fakeProvider{
		name: "sdlc", actions: []string{"sdlc.build"},
		grants: map[mantlekeep.Role][]string{mantlekeep.RoleConsumer: {"sdlc.build"}},
	}
	r := NewRBAC().WithProviders(nil, real, nil)

	if len(r.providers) != 1 {
		t.Fatalf("registered %d providers, want 1 (nils skipped)", len(r.providers))
	}
	if r.byAction["sdlc.build"] == nil {
		t.Error("the real provider was lost among the nils")
	}
}

func TestWithProvidersReturnsTheReceiverToChain(t *testing.T) {
	r := NewRBAC()
	if got := r.WithProviders(fakeProvider{name: "x"}); got != r {
		t.Error("WithProviders did not return the receiver, so the builder chain breaks")
	}
}

// The wiring is only worth anything if a decision uses it: a provider's grant must let
// its action through the generic role check, and its Admit must be able to refuse.
func TestAProviderGrantAuthorizesAndItsAdmitCanStillRefuse(t *testing.T) {
	allowed := fakeProvider{
		name: "sdlc", actions: []string{"sdlc.build"},
		grants: map[mantlekeep.Role][]string{mantlekeep.RoleConsumer: {"sdlc.build"}},
	}
	r := NewRBAC().WithProviders(allowed)
	roles := []string{string(mantlekeep.RoleConsumer)}

	if !r.actionAllowed(roles, "sdlc.build", nil) {
		t.Error("a provider-granted action was refused by the generic role check")
	}
	if r.actionAllowed(roles, "sdlc.release", nil) {
		t.Error("an action no provider granted was allowed")
	}
}
