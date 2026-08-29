package kafkagrant

import (
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kmsg"
)

func testBoundary() Boundary {
	return Boundary{
		Principal: "User:svc-payments",
		Prefix:    "payments.",
		Quota:     Quota{ProducerByteRate: 10 << 20, ConsumerByteRate: 20 << 20},
	}
}

// TestBoundaryACLsArePrefixedNeverLiteral guards the decision that keeps onboarding rare.
// A LITERAL binding per topic is the ACL sprawl this model exists to escape.
func TestBoundaryACLsArePrefixedNeverLiteral(t *testing.T) {
	bindings, err := PlanBoundaryACLs(testBoundary())
	if err != nil {
		t.Fatalf("PlanBoundaryACLs: %v", err)
	}
	if len(bindings) == 0 {
		t.Fatal("no bindings planned — the assertions below would pass vacuously")
	}
	for _, binding := range bindings {
		if binding.Pattern != kmsg.ACLResourcePatternTypePrefixed {
			t.Errorf("binding %s on %s %q has pattern %s, want PREFIXED — a LITERAL binding per topic "+
				"recreates ACL sprawl and makes every playground topic a governance event",
				binding.Operation, binding.ResourceType, binding.ResourceName, binding.Pattern)
		}
		if binding.Pattern == kmsg.ACLResourcePatternTypeLiteral {
			t.Errorf("binding %s on %s %q is LITERAL", binding.Operation, binding.ResourceType, binding.ResourceName)
		}
		if binding.ResourceName != "payments." {
			t.Errorf("binding resource name = %q, want the prefix %q", binding.ResourceName, "payments.")
		}
		if binding.Permission != kmsg.ACLPermissionTypeAllow {
			t.Errorf("binding %s has permission %s, want ALLOW", binding.Operation, binding.Permission)
		}
		if binding.Principal != "User:svc-payments" {
			t.Errorf("binding principal = %q, want %q", binding.Principal, "User:svc-payments")
		}
	}
}

// TestBoundaryACLsWithholdCreate is the load-bearing assertion of this package. The team
// may read and write everything under its prefix and still cannot bring a topic into
// existence — topic creation stays a governed act, routed through Provision.
func TestBoundaryACLsWithholdCreate(t *testing.T) {
	bindings, err := PlanBoundaryACLs(testBoundary())
	if err != nil {
		t.Fatalf("PlanBoundaryACLs: %v", err)
	}
	for _, binding := range bindings {
		switch binding.Operation {
		case kmsg.ACLOperationCreate:
			t.Fatalf("a CREATE binding was planned on %s %q — the team must not be able to create topics; "+
				"the thing to add is a Provision call, not an operation",
				binding.ResourceType, binding.ResourceName)
		case kmsg.ACLOperationAll:
			t.Fatalf("an ALL binding was planned on %s %q — ALL subsumes CREATE",
				binding.ResourceType, binding.ResourceName)
		case kmsg.ACLOperationAlter, kmsg.ACLOperationDelete, kmsg.ACLOperationAlterConfigs:
			t.Fatalf("binding %s on %s %q grants more than the namespace model does",
				binding.Operation, binding.ResourceType, binding.ResourceName)
		}
	}
}

func TestBoundaryACLsGrantExactlyTheNamespaceOperations(t *testing.T) {
	bindings, err := PlanBoundaryACLs(testBoundary())
	if err != nil {
		t.Fatalf("PlanBoundaryACLs: %v", err)
	}
	topicOperations := map[kmsg.ACLOperation]bool{}
	groupOperations := map[kmsg.ACLOperation]bool{}
	for _, binding := range bindings {
		switch binding.ResourceType {
		case kmsg.ACLResourceTypeTopic:
			topicOperations[binding.Operation] = true
		case kmsg.ACLResourceTypeGroup:
			groupOperations[binding.Operation] = true
		default:
			t.Fatalf("unexpected resource type %s — the namespace model covers topic and group only", binding.ResourceType)
		}
	}
	wantTopic := map[kmsg.ACLOperation]bool{
		kmsg.ACLOperationRead:     true,
		kmsg.ACLOperationWrite:    true,
		kmsg.ACLOperationDescribe: true,
	}
	wantGroup := map[kmsg.ACLOperation]bool{kmsg.ACLOperationRead: true}
	assertOperationSet(t, "topic", topicOperations, wantTopic)
	assertOperationSet(t, "group", groupOperations, wantGroup)
}

func assertOperationSet(t *testing.T, kind string, got, want map[kmsg.ACLOperation]bool) {
	t.Helper()
	for operation := range want {
		if !got[operation] {
			t.Errorf("%s operations are missing %s", kind, operation)
		}
	}
	for operation := range got {
		if !want[operation] {
			t.Errorf("%s operations include %s, which the namespace model does not grant", kind, operation)
		}
	}
}

func TestPlanBoundaryACLsRefusesAnUnboundedPrefix(t *testing.T) {
	boundary := testBoundary()
	boundary.Prefix = ""
	if _, err := PlanBoundaryACLs(boundary); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("PlanBoundaryACLs with an empty prefix = %v, want ErrInvalidGrant — "+
			"a PREFIXED binding on \"\" would cover the whole cluster", err)
	}
}

func TestPlanQuotaUsesTheCallersNumbersAndTheEntityName(t *testing.T) {
	alteration, err := PlanQuota(testBoundary())
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	if alteration.EntityType != "user" {
		t.Errorf("entity type = %q, want %q", alteration.EntityType, "user")
	}
	// The quota entity is keyed on the bare name — NOT the "User:" ACL form.
	if alteration.EntityName != "svc-payments" {
		t.Errorf("entity name = %q, want %q (the ACL form would silently write a quota for a user that does not exist)",
			alteration.EntityName, "svc-payments")
	}
	values := map[string]float64{}
	for _, value := range alteration.Values {
		values[value.Key] = value.Value
	}
	if got := values[ProducerByteRateKey]; got != float64(10<<20) {
		t.Errorf("%s = %v, want %v — the adapter must pass the caller's number through, not choose one",
			ProducerByteRateKey, got, float64(10<<20))
	}
	if got := values[ConsumerByteRateKey]; got != float64(20<<20) {
		t.Errorf("%s = %v, want %v", ConsumerByteRateKey, got, float64(20<<20))
	}
	if len(alteration.Values) != 2 {
		t.Errorf("planned %d quota values, want exactly the 2 the caller supplied: %+v", len(alteration.Values), alteration.Values)
	}
}

func TestPlanTopicAppliesTheRetentionFloorPassedIn(t *testing.T) {
	spec, err := PlanTopic(Grant{
		Principal:         "User:svc-payments",
		Prefix:            "payments.",
		Topic:             "payments.settlement.v1",
		Partitions:        6,
		ReplicationFactor: 3,
		RetentionMillis:   604800000,
	})
	if err != nil {
		t.Fatalf("PlanTopic: %v", err)
	}
	if spec.Configs[RetentionMillisConfig] != "604800000" {
		t.Fatalf("%s = %q, want %q", RetentionMillisConfig, spec.Configs[RetentionMillisConfig], "604800000")
	}
	if spec.Partitions != 6 || spec.ReplicationFactor != 3 {
		t.Fatalf("partitions/rf = %d/%d, want 6/3 — these are the caller's numbers", spec.Partitions, spec.ReplicationFactor)
	}
}

func TestPlanTopicInventsNoRetentionWhenNoneWasGiven(t *testing.T) {
	spec, err := PlanTopic(Grant{
		Principal:         "User:svc-payments",
		Prefix:            "payments.",
		Topic:             "payments.scratch",
		Partitions:        -1,
		ReplicationFactor: -1,
	})
	if err != nil {
		t.Fatalf("PlanTopic: %v", err)
	}
	if _, set := spec.Configs[RetentionMillisConfig]; set {
		t.Fatalf("%s was set to %q with no retention requested — deciding a limit is the caller's floor, not the adapter's",
			RetentionMillisConfig, spec.Configs[RetentionMillisConfig])
	}
}

func TestPlanTopicRefusesATopicOutsideTheGrantedPrefix(t *testing.T) {
	_, err := PlanTopic(Grant{
		Principal:  "User:svc-payments",
		Prefix:     "payments.",
		Topic:      "ledger.settlement.v1",
		Partitions: -1,
	})
	if !errors.Is(err, ErrOutsideBoundary) {
		t.Fatalf("PlanTopic outside the prefix = %v, want ErrOutsideBoundary", err)
	}
}
