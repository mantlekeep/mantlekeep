package kafkagrant

import (
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// The artifacts below are EVIDENCE, and the rule that makes them evidence is that every
// field describing cluster state was READ BACK from the cluster after the change, never
// copied from the request. A result assembled from its own input reports success
// whenever the request was well formed, which is exactly the case where you least want
// to be told success.
//
// IntentID and PolicyID are the two exceptions, and they are not exceptions to the rule:
// they attribute the DECISION, which the cluster has no knowledge of and could never be
// asked about. They come from the execution token the door issued.

// BoundaryArtifact is what the cluster reports after a team was onboarded.
type BoundaryArtifact struct {
	IntentID  string    // the approved intent this applies under (from the execution token)
	PolicyID  string    // which policy authorised it (from the execution token)
	Principal Principal // the principal the namespace belongs to
	Prefix    string    // the prefix it owns

	// ACLs are the bindings DescribeACLs returned for the principal on the prefix. If
	// this is shorter than what was planned, the cluster did not accept everything —
	// which is a fact worth having and one an echoed result would have hidden.
	ACLs []ACLBinding

	// Quota is what DescribeClientQuotas returned for the principal.
	Quota []QuotaValue

	ObservedAt time.Time // when the read-back happened
}

// GrantsCreate reports whether the cluster holds a binding that lets this principal
// create topics under this prefix — CREATE itself, or ALL, which subsumes it.
//
// It should always be false. This adapter never writes either one, so a true here means
// something OUTSIDE this adapter granted it. That is precisely the fact the read-back
// exists to surface: the question is what the cluster permits, not what this call wrote.
func (a BoundaryArtifact) GrantsCreate() bool {
	for _, binding := range a.ACLs {
		if binding.Permission != kmsg.ACLPermissionTypeAllow {
			continue
		}
		if binding.Operation == kmsg.ACLOperationCreate || binding.Operation == kmsg.ACLOperationAll {
			return true
		}
	}
	return false
}

// TopicArtifact is what the cluster reports after a topic was provisioned.
type TopicArtifact struct {
	IntentID string // the approved intent this applies under (from the execution token)
	PolicyID string // which policy authorised it (from the execution token)
	Topic    string

	// Config is the topic configuration DescribeTopicConfigs returned. Read the applied
	// retention from here, not from the grant — the broker may hold a different value
	// (see RetentionMillis and AlreadyExisted).
	Config map[string]string

	// AlreadyExisted is true when the topic was present before this call. Provisioning is
	// idempotent, so this is a SUCCESS; the field exists so the caller can tell a
	// no-op re-run apart from a first creation.
	AlreadyExisted bool

	ObservedAt time.Time
}

// RetentionMillis returns the retention the cluster actually holds for this topic, and
// whether the topic carried that config at all.
//
// This matters most when AlreadyExisted is true. This adapter sets retention when it
// CREATES a topic; it does not rewrite the retention of a topic that already exists and
// already holds data — shortening retention deletes records, which is a separate act
// deserving its own approval rather than a side effect of an idempotent re-run. So an
// existing topic whose retention differs from the requested floor is REPORTED here, not
// silently corrected.
func (a TopicArtifact) RetentionMillis() (string, bool) {
	value, ok := a.Config[RetentionMillisConfig]
	return value, ok
}
