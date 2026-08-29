package franz

import (
	"errors"
	"fmt"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kerr"
)

// mapCreateTopicError translates the broker's create-topic outcome into this module's
// vocabulary.
//
// TOPIC_ALREADY_EXISTS is the one that matters: kafkagrant treats it as SUCCESS, because
// provisioning is idempotent and a replayed grant should converge rather than error. The
// translation lives HERE so kafkagrant never has to know a Kafka error code — and so
// this mapping can be tested against the real kerr value with no broker running.
func mapCreateTopicError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, kerr.TopicAlreadyExists) {
		// Both are wrapped: the sentinel so kafkagrant can branch on it, and the broker's
		// own error so a caller diagnosing something else still has the original. Wrapping
		// only the sentinel would silently throw the broker's answer away.
		return fmt.Errorf("%w: %w", kafkagrant.ErrTopicExists, err)
	}
	return err
}
