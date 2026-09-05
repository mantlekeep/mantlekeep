package kafkagrant

import (
	"strconv"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// TopicOperations are the operations a team gets on its own topic prefix.
//
// CREATE IS DELIBERATELY ABSENT, and that absence is the load-bearing decision in this
// package.
//
// With READ, WRITE and DESCRIBE on the prefix the team can do everything with a topic
// that already exists: produce to it, consume from it, inspect it — with no approval and
// no ticket. What it cannot do is bring a topic INTO EXISTENCE. Topic creation stays a
// governed act, routed through Provision, so the set of topics on the cluster remains
// something that was decided rather than something that accumulated.
//
// That line is drawn here rather than at the broker's edge for a reason. Granting CREATE
// on the prefix would be simpler and would look identical on day one; it would also mean
// the cluster's topic inventory is whatever every client library's auto-create happened
// to produce, permanently, with no record of who asked for what. Withholding one
// operation is what makes the frequent path free WITHOUT making the namespace unbounded.
//
// If you are tempted to add OpCreate here to unblock something: the thing to add is a
// Provision call, not an operation.
var TopicOperations = []kmsg.ACLOperation{
	kmsg.ACLOperationRead,
	kmsg.ACLOperationWrite,
	kmsg.ACLOperationDescribe,
}

// GroupOperations are the operations a team gets on its own consumer-group prefix.
// READ is what joining a group requires; nothing more is needed to consume.
var GroupOperations = []kmsg.ACLOperation{
	kmsg.ACLOperationRead,
}

// Kafka's client-quota configuration keys and the entity type they are keyed on.
const (
	QuotaEntityTypeUser   = "user"
	ProducerByteRateKey   = "producer_byte_rate"
	ConsumerByteRateKey   = "consumer_byte_rate"
	RetentionMillisConfig = "retention.ms"
)

// anyHost is the host part of every binding written here. Host-based restriction is a
// network control, not a namespace control, and pretending otherwise would put a second
// half-enforced boundary in a place nobody looks.
const anyHost = "*"

// PlanBoundaryACLs builds the ACL bindings that give a team its namespace: the topic
// operations and the group operations, both PREFIXED over the boundary's prefix.
//
// PREFIXED, never LITERAL. A LITERAL binding per topic is the ACL sprawl this model
// exists to escape: the rule count grows with the topic count forever, every new topic
// becomes a governance event, and the golden path ends up slower than working around it.
// One prefixed binding per operation covers the namespace for its whole life.
//
// The boundary is validated first — an unvalidated boundary with an empty prefix would
// produce bindings covering the entire cluster.
func PlanBoundaryACLs(boundary Boundary) ([]ACLBinding, error) {
	if err := boundary.Validate(); err != nil {
		return nil, err
	}
	bindings := make([]ACLBinding, 0, len(TopicOperations)+len(GroupOperations))
	for _, operation := range TopicOperations {
		bindings = append(bindings, prefixedAllow(boundary, kmsg.ACLResourceTypeTopic, operation))
	}
	for _, operation := range GroupOperations {
		bindings = append(bindings, prefixedAllow(boundary, kmsg.ACLResourceTypeGroup, operation))
	}
	return bindings, nil
}

func prefixedAllow(boundary Boundary, resourceType kmsg.ACLResourceType, operation kmsg.ACLOperation) ACLBinding {
	return ACLBinding{
		Principal:    boundary.Principal,
		Host:         anyHost,
		ResourceType: resourceType,
		ResourceName: boundary.Prefix,
		Pattern:      kmsg.ACLResourcePatternTypePrefixed,
		Operation:    operation,
		Permission:   kmsg.ACLPermissionTypeAllow,
	}
}

// PlanQuota builds the client-quota alteration for the boundary's principal.
//
// The entity is keyed on the principal's NAME, not its "User:name" form — the ACL wire
// format and the quota wire format disagree about this, and getting it wrong writes a
// quota against a user that does not exist, which the broker accepts silently.
func PlanQuota(boundary Boundary) (QuotaAlteration, error) {
	if err := boundary.Validate(); err != nil {
		return QuotaAlteration{}, err
	}
	return QuotaAlteration{
		EntityType: QuotaEntityTypeUser,
		EntityName: boundary.Principal.Name(),
		Values: []QuotaValue{
			{Key: ProducerByteRateKey, Value: float64(boundary.Quota.ProducerByteRate)},
			{Key: ConsumerByteRateKey, Value: float64(boundary.Quota.ConsumerByteRate)},
		},
	}, nil
}

// PlanTopic builds the topic to create from a grant, applying the retention floor the
// caller passed in. A zero retention leaves the broker's own default in place rather
// than inventing one — this adapter does not decide limits.
func PlanTopic(grant Grant) (TopicSpec, error) {
	if err := grant.Validate(); err != nil {
		return TopicSpec{}, err
	}
	spec := TopicSpec{
		Name:              grant.Topic,
		Partitions:        grant.Partitions,
		ReplicationFactor: grant.ReplicationFactor,
	}
	if grant.RetentionMillis > 0 {
		spec.Configs = map[string]string{
			RetentionMillisConfig: strconv.FormatInt(grant.RetentionMillis, 10),
		}
	}
	return spec, nil
}
