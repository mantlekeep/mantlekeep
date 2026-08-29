package kafkagrant

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func approvedToken() mantlekeep.ExecutionToken {
	return mantlekeep.ExecutionToken{
		Value:     "opaque",
		IntentID:  "INT-2026-08-001",
		Scope:     "payments.",
		PolicyID:  "acme.rbac",
		IssuedAt:  time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func testGrant() Grant {
	return Grant{
		Principal:         "User:svc-payments",
		Prefix:            "payments.",
		Topic:             "payments.settlement.v1",
		Partitions:        6,
		ReplicationFactor: 3,
		RetentionMillis:   604800000,
	}
}

// TestProvisionRefusesATopicOutsideTheBoundaryBeforeTouchingTheCluster is the defence in
// depth. The caller is expected to have checked too; this check exists so a bug upstream
// cannot become a permission nobody approved.
func TestProvisionRefusesATopicOutsideTheBoundaryBeforeTouchingTheCluster(t *testing.T) {
	admin := &fakeAdmin{}
	grant := testGrant()
	grant.Topic = "ledger.settlement.v1"

	_, err := NewAdapter(admin).Provision(context.Background(), approvedToken(), grant)

	if !errors.Is(err, ErrOutsideBoundary) {
		t.Fatalf("Provision = %v, want ErrOutsideBoundary", err)
	}
	if mutations := admin.mutatingCalls(); len(mutations) != 0 {
		t.Fatalf("the cluster was changed before the refusal: %v — a refusal must abort before the side effect", mutations)
	}
}

// TestProvisionTreatsAnExistingTopicAsSuccess — provisioning is idempotent. A replayed
// grant must converge, not error.
func TestProvisionTreatsAnExistingTopicAsSuccess(t *testing.T) {
	admin := &fakeAdmin{
		// Wrapped, exactly as the franz adapter wraps the broker's TOPIC_ALREADY_EXISTS.
		createTopicErr:  fmt.Errorf("%w: TOPIC_ALREADY_EXISTS", ErrTopicExists),
		describedConfig: map[string]string{RetentionMillisConfig: "604800000"},
	}

	artifact, err := NewAdapter(admin).Provision(context.Background(), approvedToken(), testGrant())

	if err != nil {
		t.Fatalf("Provision on an existing topic = %v, want success", err)
	}
	if !artifact.AlreadyExisted {
		t.Error("AlreadyExisted = false, want true — the caller must be able to tell a no-op re-run from a first creation")
	}
	if retention, ok := artifact.RetentionMillis(); !ok || retention != "604800000" {
		t.Errorf("retention read back = %q (present=%v), want %q", retention, ok, "604800000")
	}
}

func TestProvisionSurfacesARealCreateFailure(t *testing.T) {
	admin := &fakeAdmin{createTopicErr: errBroker}

	_, err := NewAdapter(admin).Provision(context.Background(), approvedToken(), testGrant())

	if !errors.Is(err, errBroker) {
		t.Fatalf("Provision = %v, want the broker error — only TOPIC_ALREADY_EXISTS is success", err)
	}
}

// TestProvisionReportsTheClustersConfigNotTheRequests — a result echoed from its own
// input is testimony, not evidence.
func TestProvisionReportsTheClustersConfigNotTheRequests(t *testing.T) {
	admin := &fakeAdmin{
		// The "cluster" disagrees with the request: a pre-existing topic whose retention
		// is not the floor that was asked for.
		createTopicErr: fmt.Errorf("%w", ErrTopicExists),
		describedConfig: map[string]string{
			RetentionMillisConfig: "86400000",
			"cleanup.policy":      "delete",
		},
	}

	artifact, err := NewAdapter(admin).Provision(context.Background(), approvedToken(), testGrant())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	retention, ok := artifact.RetentionMillis()
	if !ok {
		t.Fatal("no retention in the artifact")
	}
	if retention == "604800000" {
		t.Fatal("the artifact reported the REQUESTED retention — it must report what the cluster holds")
	}
	if retention != "86400000" {
		t.Fatalf("retention = %q, want the cluster's %q", retention, "86400000")
	}
	if artifact.Config["cleanup.policy"] != "delete" {
		t.Errorf("the artifact dropped a config the cluster reported: %+v", artifact.Config)
	}
	if artifact.IntentID != "INT-2026-08-001" || artifact.PolicyID != "acme.rbac" {
		t.Errorf("artifact = intent %q policy %q, want the decision the token attributes it to",
			artifact.IntentID, artifact.PolicyID)
	}
	if artifact.ObservedAt.IsZero() {
		t.Error("ObservedAt is zero — evidence needs to say when it was observed")
	}
}

func TestOnboardTeamBoundsBeforeItPermits(t *testing.T) {
	admin := &fakeAdmin{}

	if _, err := NewAdapter(admin).OnboardTeam(context.Background(), approvedToken(), testBoundary()); err != nil {
		t.Fatalf("OnboardTeam: %v", err)
	}

	mutations := admin.mutatingCalls()
	want := []string{"AlterClientQuota", "CreateACLs"}
	if len(mutations) != len(want) {
		t.Fatalf("mutating calls = %v, want %v", mutations, want)
	}
	for i := range want {
		if mutations[i] != want[i] {
			t.Fatalf("mutating calls = %v, want %v — the quota must land before the ACLs, or there is a "+
				"window in which the team can produce without a bound", mutations, want)
		}
	}
}

// TestOnboardTeamArtifactComesFromTheClusterNotTheRequest loads the fake with a cluster
// state that differs from what was written, and proves the artifact follows the cluster.
func TestOnboardTeamArtifactComesFromTheClusterNotTheRequest(t *testing.T) {
	principal := Principal("User:svc-payments")
	admin := &fakeAdmin{
		// The cluster reports only ONE of the four bindings that were written.
		describedACLs: []ACLBinding{
			allowBinding(principal, kmsg.ACLResourceTypeTopic, "payments.", kmsg.ACLResourcePatternTypePrefixed, kmsg.ACLOperationRead),
		},
		describedQuota: []QuotaValue{{Key: ProducerByteRateKey, Value: 1024}},
	}

	artifact, err := NewAdapter(admin).OnboardTeam(context.Background(), approvedToken(), testBoundary())
	if err != nil {
		t.Fatalf("OnboardTeam: %v", err)
	}

	if len(admin.createdACLs) != 4 {
		t.Fatalf("wrote %d bindings, want 4 — the fixture below depends on write and read-back differing", len(admin.createdACLs))
	}
	if len(artifact.ACLs) != 1 {
		t.Fatalf("artifact has %d ACLs, want the 1 the cluster reported — an artifact must not be assembled "+
			"from the request", len(artifact.ACLs))
	}
	if len(artifact.Quota) != 1 || artifact.Quota[0].Value != 1024 {
		t.Fatalf("artifact quota = %+v, want the cluster's single 1024 value, not the requested pair", artifact.Quota)
	}
}

// TestGrantsCreateSurfacesAnExternallyGrantedCreate — the read-back's real value: it
// reports what the cluster permits, including a permission this adapter never wrote.
func TestGrantsCreateSurfacesAnExternallyGrantedCreate(t *testing.T) {
	principal := Principal("User:svc-payments")
	clean := BoundaryArtifact{ACLs: []ACLBinding{
		allowBinding(principal, kmsg.ACLResourceTypeTopic, "payments.", kmsg.ACLResourcePatternTypePrefixed, kmsg.ACLOperationWrite),
	}}
	if clean.GrantsCreate() {
		t.Error("GrantsCreate() = true on a namespace holding only WRITE")
	}

	for _, operation := range []kmsg.ACLOperation{kmsg.ACLOperationCreate, kmsg.ACLOperationAll} {
		polluted := BoundaryArtifact{ACLs: []ACLBinding{
			allowBinding(principal, kmsg.ACLResourceTypeTopic, "payments.", kmsg.ACLResourcePatternTypePrefixed, operation),
		}}
		if !polluted.GrantsCreate() {
			t.Errorf("GrantsCreate() = false with an ALLOW %s binding on the cluster", operation)
		}
	}
}

func TestExpiredApprovalStopsApplying(t *testing.T) {
	expired := approvedToken()
	expired.ExpiresAt = time.Now().Add(-time.Minute)

	for name, apply := range map[string]func(*Adapter) error{
		"OnboardTeam": func(a *Adapter) error {
			_, err := a.OnboardTeam(context.Background(), expired, testBoundary())
			return err
		},
		"Provision": func(a *Adapter) error {
			_, err := a.Provision(context.Background(), expired, testGrant())
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			admin := &fakeAdmin{}
			err := apply(NewAdapter(admin))
			if !errors.Is(err, ErrNotApproved) {
				t.Fatalf("%s with an expired token = %v, want ErrNotApproved", name, err)
			}
			if mutations := admin.mutatingCalls(); len(mutations) != 0 {
				t.Fatalf("the cluster was changed under an expired approval: %v", mutations)
			}
		})
	}
}

func TestApplyWithoutATokenIsRefused(t *testing.T) {
	admin := &fakeAdmin{}
	_, err := NewAdapter(admin).Provision(context.Background(), mantlekeep.ExecutionToken{}, testGrant())
	if !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Provision with a zero token = %v, want ErrNotApproved", err)
	}
	if mutations := admin.mutatingCalls(); len(mutations) != 0 {
		t.Fatalf("the cluster was changed with no token at all: %v", mutations)
	}
}

func TestNewAdapterRefusesANilAdmin(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewAdapter(nil) did not panic — a wiring error must fail at construction")
		}
	}()
	NewAdapter(nil)
}
