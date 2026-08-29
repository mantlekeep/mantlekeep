package kafkagrant

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// fakeAdmin is a stand-in for a Kafka cluster. It records the calls made against it, in
// order, so a test can assert not only WHAT was applied but that nothing was applied at
// all when the adapter should have refused — which is the whole point of governing
// before executing.
//
// It deliberately does NOT echo the request into the describe results: describeACLs and
// describeQuota return whatever the test loaded into them, so a test can make the
// "cluster" disagree with the request and prove the artifact follows the cluster.
type fakeAdmin struct {
	calls []string

	createdACLs   []ACLBinding
	alteredQuota  QuotaAlteration
	createdTopics []TopicSpec

	describedACLs   []ACLBinding
	describedQuota  []QuotaValue
	describedConfig map[string]string

	createTopicErr error
	createACLsErr  error
	alterQuotaErr  error
	describeErr    error
}

func (f *fakeAdmin) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeAdmin) CreateACLs(_ context.Context, bindings []ACLBinding) error {
	f.record("CreateACLs")
	if f.createACLsErr != nil {
		return f.createACLsErr
	}
	f.createdACLs = append(f.createdACLs, bindings...)
	return nil
}

func (f *fakeAdmin) DescribeACLs(_ context.Context, _ Principal, _ string, _ kmsg.ACLResourcePatternType) ([]ACLBinding, error) {
	f.record("DescribeACLs")
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return f.describedACLs, nil
}

func (f *fakeAdmin) AlterClientQuota(_ context.Context, alteration QuotaAlteration) error {
	f.record("AlterClientQuota")
	if f.alterQuotaErr != nil {
		return f.alterQuotaErr
	}
	f.alteredQuota = alteration
	return nil
}

func (f *fakeAdmin) DescribeClientQuota(_ context.Context, _, _ string) ([]QuotaValue, error) {
	f.record("DescribeClientQuota")
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return f.describedQuota, nil
}

func (f *fakeAdmin) CreateTopic(_ context.Context, spec TopicSpec) error {
	f.record("CreateTopic")
	if f.createTopicErr != nil {
		return f.createTopicErr
	}
	f.createdTopics = append(f.createdTopics, spec)
	return nil
}

func (f *fakeAdmin) DescribeTopicConfig(_ context.Context, _ string) (map[string]string, error) {
	f.record("DescribeTopicConfig")
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return f.describedConfig, nil
}

// mutatingCalls returns only the calls that changed the cluster. A refusal must leave
// this empty: a deny has to abort BEFORE the side effect, not roll it back after.
func (f *fakeAdmin) mutatingCalls() []string {
	var mutations []string
	for _, call := range f.calls {
		switch call {
		case "CreateACLs", "AlterClientQuota", "CreateTopic":
			mutations = append(mutations, call)
		}
	}
	return mutations
}

// allowBinding builds a binding as a cluster would report it, for loading into the fake's
// describe results.
func allowBinding(principal Principal, resource kmsg.ACLResourceType, name string, pattern kmsg.ACLResourcePatternType, operation kmsg.ACLOperation) ACLBinding {
	return ACLBinding{
		Principal:    principal,
		Host:         "*",
		ResourceType: resource,
		ResourceName: name,
		Pattern:      pattern,
		Operation:    operation,
		Permission:   kmsg.ACLPermissionTypeAllow,
	}
}

var _ Admin = (*fakeAdmin)(nil)

// errBroker stands in for any transport-level failure from the cluster.
var errBroker = fmt.Errorf("broker unavailable")
