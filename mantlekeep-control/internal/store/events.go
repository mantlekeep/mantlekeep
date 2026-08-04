package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"
	"mantlekeep.dev/control/orchestrator"
)

var eventsBucket = []byte("events")

// BoltEvents is a PERSISTENT orchestrator.EventStore over a bbolt file — the durable
// twin of the in-memory MemStore. It is what makes a run's timeline survive a
// restart: close the laptop, reopen, `mantlekeep serve`, and the history is still there.
// Same embedded, no-server, single-file model as the audit chain (and sqlite).
type BoltEvents struct{ db *bolt.DB }

// OpenBoltEvents opens (or creates) a bbolt-backed event store at path.
func OpenBoltEvents(path string) (*BoltEvents, error) {
	if path == "" {
		path = "mantlekeep-events.db"
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists(eventsBucket)
		return e
	}); err != nil {
		_ = db.Close() // best-effort cleanup; the bucket-create error is what we report
		return nil, err
	}
	return &BoltEvents{db: db}, nil
}

// Append stamps a store-wide monotonic Seq (bbolt's own sequence, atomic within the
// write txn) and persists the event keyed in Seq order.
func (s *BoltEvents) Append(_ context.Context, e orchestrator.Event) (orchestrator.Event, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(eventsBucket)
		seq, _ := bkt.NextSequence()
		// bbolt's sequence is uint64; Event.Seq is int. Guard the narrowing so a run that
		// somehow reached math.MaxInt events fails loudly instead of wrapping to a negative
		// (and mis-ordering the timeline). In practice unreachable; correctness over trust.
		if seq > math.MaxInt {
			return fmt.Errorf("event sequence %d exceeds max int", seq)
		}
		e.Seq = int(seq)
		val, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return bkt.Put([]byte(fmt.Sprintf("%016d", seq)), val)
	})
	if err != nil {
		return orchestrator.Event{}, err
	}
	return e, nil
}

// Events returns a run's events in Seq (insertion) order — all runs when run == "".
// The zero-padded keys make the cursor iterate in Seq order without an explicit sort.
func (s *BoltEvents) Events(_ context.Context, run string) ([]orchestrator.Event, error) {
	var out []orchestrator.Event
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(eventsBucket).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var e orchestrator.Event
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			if run == "" || e.Run == run {
				out = append(out, e)
			}
		}
		return nil
	})
	return out, err
}

// Close releases the bbolt file.
func (s *BoltEvents) Close() error { return s.db.Close() }
