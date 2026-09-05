package franz

import (
	"errors"
	"testing"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kerr"
)

// TestAlreadyExistsBecomesTheIdempotencySentinel checks the mapping against the REAL
// Kafka error value — the one a broker's error code 36 resolves to — so idempotency is
// proven against the protocol rather than against a hand-written stub. No broker needed.
func TestAlreadyExistsBecomesTheIdempotencySentinel(t *testing.T) {
	fromBrokerErrorCode := kerr.ErrorForCode(36) // TOPIC_ALREADY_EXISTS
	if !errors.Is(fromBrokerErrorCode, kerr.TopicAlreadyExists) {
		t.Fatalf("error code 36 resolved to %v, not TopicAlreadyExists — the fixture is wrong", fromBrokerErrorCode)
	}

	mapped := mapCreateTopicError(fromBrokerErrorCode)
	if !errors.Is(mapped, kafkagrant.ErrTopicExists) {
		t.Fatalf("mapCreateTopicError(TOPIC_ALREADY_EXISTS) = %v, want it to wrap ErrTopicExists — "+
			"without this, a replayed grant fails instead of converging", mapped)
	}
	// The original stays reachable: the mapping adds vocabulary, it does not discard detail.
	if !errors.Is(mapped, kerr.TopicAlreadyExists) {
		t.Errorf("the broker's error was discarded by the mapping: %v", mapped)
	}
}

func TestOtherBrokerErrorsAreNotSwallowed(t *testing.T) {
	for _, brokerError := range []error{
		kerr.TopicAuthorizationFailed,
		kerr.InvalidPartitions,
		kerr.InvalidReplicationFactor,
		errors.New("dial tcp: connection refused"),
	} {
		mapped := mapCreateTopicError(brokerError)
		if errors.Is(mapped, kafkagrant.ErrTopicExists) {
			t.Errorf("mapCreateTopicError(%v) was treated as already-exists — only TOPIC_ALREADY_EXISTS is success", brokerError)
		}
		if !errors.Is(mapped, brokerError) {
			t.Errorf("mapCreateTopicError(%v) lost the original error: %v", brokerError, mapped)
		}
	}
}

func TestNilStaysNil(t *testing.T) {
	if err := mapCreateTopicError(nil); err != nil {
		t.Fatalf("mapCreateTopicError(nil) = %v, want nil", err)
	}
}
