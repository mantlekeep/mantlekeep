package franz

import (
	"testing"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// The namespace model writes ONE request per (resource type) rather than one per operation:
// the four planned bindings collapse into a topic group carrying READ/WRITE/DESCRIBE and a
// consumer-group group carrying READ.
func TestGroupBindingsCollapsesOperationsPerResourceType(t *testing.T) {
	const prefix = "payments."

	boundary := kafkagrant.Boundary{
		Principal: "User:svc-payments",
		Prefix:    prefix,
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
		assertGroupIsPrefixedOn(t, group, prefix)
		assertGroupOperationCount(t, group)
	}
}

// Every group must cover exactly the one prefix the boundary named, as a PREFIXED pattern —
// a LITERAL pattern here would mean an ACL per topic, which is the sprawl the model avoids.
func assertGroupIsPrefixedOn(t *testing.T, group *aclGroup, prefix string) {
	t.Helper()
	if len(group.names) != 1 || group.names[0] != prefix {
		t.Errorf("group names = %v, want the single prefix %q", group.names, prefix)
	}
	if group.key.pattern != kmsg.ACLResourcePatternTypePrefixed {
		t.Errorf("group pattern = %s, want PREFIXED", group.key.pattern)
	}
}

// assertGroupOperationCount checks the collapse itself: how many operations ended up on the
// one request for this resource type. A resource type the model does not write is a failure
// in its own right — CREATE is deliberately never granted, so an unexpected type here would
// mean the plan grew a permission nobody asked for.
func assertGroupOperationCount(t *testing.T, group *aclGroup) {
	t.Helper()
	wantOperations := map[kmsg.ACLResourceType]int{
		kmsg.ACLResourceTypeTopic: 3, // READ, WRITE, DESCRIBE
		kmsg.ACLResourceTypeGroup: 1, // READ
	}
	want, expected := wantOperations[group.key.resource]
	if !expected {
		t.Errorf("unexpected resource type %s", group.key.resource)
		return
	}
	if len(group.operations) != want {
		t.Errorf("%s group has %d operations %v, want %d",
			group.key.resource, len(group.operations), group.operations, want)
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
