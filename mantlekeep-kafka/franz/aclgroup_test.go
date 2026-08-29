package franz

import (
	"testing"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestGroupBindingsCollapsesOperationsPerResourceType(t *testing.T) {
	boundary := kafkagrant.Boundary{
		Principal: "User:svc-payments",
		Prefix:    "payments.",
		Quota:     kafkagrant.Quota{ProducerByteRate: 1 << 20, ConsumerByteRate: 1 << 20},
	}
	bindings, err := kafkagrant.PlanBoundaryACLs(boundary)
	if err != nil {
		t.Fatalf("PlanBoundaryACLs: %v", err)
	}

	groups := groupBindings(bindings)
	if len(groups) != 2 {
		t.Fatalf("grouped 4 bindings into %d groups, want 2 (topic ops and group ops differ, everything else matches)", len(groups))
	}
	for _, group := range groups {
		if len(group.names) != 1 || group.names[0] != "payments." {
			t.Errorf("group names = %v, want the single prefix", group.names)
		}
		if group.key.pattern != kmsg.ACLResourcePatternTypePrefixed {
			t.Errorf("group pattern = %s, want PREFIXED", group.key.pattern)
		}
		switch group.key.resource {
		case kmsg.ACLResourceTypeTopic:
			if len(group.operations) != 3 {
				t.Errorf("topic group has %d operations %v, want 3", len(group.operations), group.operations)
			}
		case kmsg.ACLResourceTypeGroup:
			if len(group.operations) != 1 {
				t.Errorf("consumer-group group has %d operations %v, want 1", len(group.operations), group.operations)
			}
		default:
			t.Errorf("unexpected resource type %s", group.key.resource)
		}
	}
}

func TestBuilderRefusesWhatTheNamespaceModelDoesNotWrite(t *testing.T) {
	base := kafkagrant.ACLBinding{
		Principal:    "User:svc-payments",
		Host:         "*",
		ResourceType: kmsg.ACLResourceTypeTopic,
		ResourceName: "payments.",
		Pattern:      kmsg.ACLResourcePatternTypePrefixed,
		Operation:    kmsg.ACLOperationRead,
		Permission:   kmsg.ACLPermissionTypeAllow,
	}

	if _, err := groupBindings([]kafkagrant.ACLBinding{base})[0].builder(); err != nil {
		t.Fatalf("a well-formed ALLOW topic binding was refused: %v", err)
	}

	denied := base
	denied.Permission = kmsg.ACLPermissionTypeDeny
	if _, err := groupBindings([]kafkagrant.ACLBinding{denied})[0].builder(); err == nil {
		t.Error("a DENY binding was accepted — this adapter writes ALLOW bindings only")
	}

	cluster := base
	cluster.ResourceType = kmsg.ACLResourceTypeCluster
	if _, err := groupBindings([]kafkagrant.ACLBinding{cluster})[0].builder(); err == nil {
		t.Error("a CLUSTER binding was accepted — the namespace model covers topic and group only")
	}

	anyPattern := base
	anyPattern.Pattern = kmsg.ACLResourcePatternTypeAny
	if _, err := groupBindings([]kafkagrant.ACLBinding{anyPattern})[0].builder(); err == nil {
		t.Error("an ANY pattern was accepted — only LITERAL and PREFIXED can be created")
	}
}
