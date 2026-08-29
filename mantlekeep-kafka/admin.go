package kafkagrant

import (
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// Sentinel errors. A caller branches on these with errors.Is; they are part of this
// package's contract and an [Admin] implementation is expected to produce ErrTopicExists
// (see below).
var (
	// ErrInvalidGrant means the request is malformed — a bad principal, an empty prefix,
	// a name Kafka would reject, a quota that bounds nothing.
	ErrInvalidGrant = errors.New("kafkagrant: invalid grant")

	// ErrOutsideBoundary means the resource is not covered by the granted prefix. This is
	// the refusal that keeps an upstream bug from becoming a permission nobody approved.
	ErrOutsideBoundary = errors.New("kafkagrant: resource outside the granted prefix")

	// ErrNotApproved means no unexpired execution token accompanied the request.
	ErrNotApproved = errors.New("kafkagrant: no valid execution token")

	// ErrTopicExists means the topic is already present on the cluster. An [Admin]
	// implementation MUST map the broker's TOPIC_ALREADY_EXISTS to this error, because
	// this package treats it as SUCCESS: provisioning is idempotent, and a re-run that
	// fails on a topic already in the state the caller asked for is a retry hazard, not
	// a safety property.
	ErrTopicExists = errors.New("kafkagrant: topic already exists")
)

// ACLBinding is one ALLOW rule as the Kafka protocol expresses it. The enum types come
// from the protocol package rather than being redeclared here, so a test asserting
// "PREFIXED, not LITERAL" asserts the value that actually goes on the wire.
type ACLBinding struct {
	Principal    Principal
	Host         string // "*" — host-based restriction is not part of this model
	ResourceType kmsg.ACLResourceType
	ResourceName string // the PREFIX, for a prefixed binding
	Pattern      kmsg.ACLResourcePatternType
	Operation    kmsg.ACLOperation
	Permission   kmsg.ACLPermissionType
}

// QuotaAlteration is the client-quota change for one principal, expressed as the entity
// and the values the broker will hold.
type QuotaAlteration struct {
	EntityType string // "user"
	EntityName string // Principal.Name() — the quota entity is keyed on the name, not the "User:" form
	Values     []QuotaValue
}

// QuotaValue is one quota configuration key and its value.
type QuotaValue struct {
	Key   string  // e.g. "producer_byte_rate"
	Value float64 // Kafka carries quota values as float64 on the wire
}

// TopicSpec is one topic to create. Every field is caller-supplied; this package adds
// nothing to it.
type TopicSpec struct {
	Name              string
	Partitions        int32             // -1 asks the broker for its default
	ReplicationFactor int16             // -1 asks the broker for its default
	Configs           map[string]string // e.g. {"retention.ms": "604800000"}
}

// ACLAdmin creates ACL bindings and reads them back.
type ACLAdmin interface {
	CreateACLs(ctx context.Context, bindings []ACLBinding) error
	// DescribeACLs returns the ALLOW bindings the cluster actually holds for principal
	// on resourceName with pattern. It is the read-back: the artifact is built from what
	// this returns, never from what CreateACLs was asked to write.
	DescribeACLs(ctx context.Context, principal Principal, resourceName string, pattern kmsg.ACLResourcePatternType) ([]ACLBinding, error)
}

// QuotaAdmin sets a principal's client quotas and reads them back.
type QuotaAdmin interface {
	AlterClientQuota(ctx context.Context, alteration QuotaAlteration) error
	DescribeClientQuota(ctx context.Context, entityType, entityName string) ([]QuotaValue, error)
}

// TopicAdmin creates a topic and reads its configuration back.
type TopicAdmin interface {
	// CreateTopic returns ErrTopicExists (wrapped is fine — errors.Is must match) when
	// the topic is already present.
	CreateTopic(ctx context.Context, spec TopicSpec) error
	DescribeTopicConfig(ctx context.Context, topic string) (map[string]string, error)
}

// Admin is the whole seam onto a Kafka cluster's Admin API. It is composed from the
// three narrow interfaces above so a caller with only one concern can depend on only
// that one — and so a fake in a test implements only what it is exercising.
//
// The franz subpackage provides the implementation backed by a real cluster. Nothing in
// this package talks to a broker directly, which is why every rule here is unit-testable
// without one.
type Admin interface {
	ACLAdmin
	QuotaAdmin
	TopicAdmin
}
