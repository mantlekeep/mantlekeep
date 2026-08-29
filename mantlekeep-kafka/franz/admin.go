package franz

import (
	"context"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kadm"
)

// Admin implements kafkagrant.Admin against a live cluster.
type Admin struct {
	client *kadm.Client
}

// New returns an Admin over an existing kadm client. The caller owns the client's
// lifetime, its seed brokers, and its TLS/SASL configuration — this package deliberately
// takes none of those, so the credentials a door brokered are handed in rather than
// discovered here.
//
// Panics on a nil client: an admin with nothing to talk to is a wiring error, better
// caught at startup than on the first governed apply.
func New(client *kadm.Client) *Admin {
	if client == nil {
		panic("franz: New requires a non-nil kadm client")
	}
	return &Admin{client: client}
}

// compile-time proof that this really satisfies the port. Without it a signature drift
// would only surface at a call site, possibly in someone else's module.
var _ kafkagrant.Admin = (*Admin)(nil)

// CreateTopic creates one topic, mapping the broker's TOPIC_ALREADY_EXISTS to
// kafkagrant.ErrTopicExists so the caller can treat a re-run as the no-op it is.
func (a *Admin) CreateTopic(ctx context.Context, spec kafkagrant.TopicSpec) error {
	configs := make(map[string]*string, len(spec.Configs))
	for key, value := range spec.Configs {
		configs[key] = kadm.StringPtr(value)
	}
	_, err := a.client.CreateTopic(ctx, spec.Partitions, spec.ReplicationFactor, configs, spec.Name)
	return mapCreateTopicError(err)
}

// DescribeTopicConfig reads the topic's configuration back from the cluster. A config
// the broker marks sensitive comes back with no value, and is reported as an empty
// string rather than omitted — that the key is set is itself a fact worth keeping.
func (a *Admin) DescribeTopicConfig(ctx context.Context, topic string) (map[string]string, error) {
	resources, err := a.client.DescribeTopicConfigs(ctx, topic)
	if err != nil {
		return nil, err
	}
	resource, err := resources.On(topic, nil)
	if err != nil {
		return nil, err
	}
	if resource.Err != nil {
		return nil, resource.Err
	}
	config := make(map[string]string, len(resource.Configs))
	for _, entry := range resource.Configs {
		config[entry.Key] = entry.MaybeValue()
	}
	return config, nil
}
