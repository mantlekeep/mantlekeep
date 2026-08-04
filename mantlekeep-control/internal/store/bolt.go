package store

import (
	"context"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
	mantlekeep "mantlekeep.dev/control"
)

// The bbolt driver is a PERSISTENT embedded mantlekeep.Store — no external database,
// no new dependency (bbolt already backs the audit log). It survives restarts,
// so it backs durable state like in-flight decision loops.
func init() {
	Register("bolt", func(dsn string) (mantlekeep.Store, error) { return OpenBolt(dsn) })
}

var kvBucket = []byte("kv")

// Bolt is a mantlekeep.Store over a bbolt file.
type Bolt struct{ db *bolt.DB }

// OpenBolt opens (or creates) a bbolt-backed store at path.
func OpenBolt(path string) (*Bolt, error) {
	if path == "" {
		path = "mantlekeep-store.db"
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists(kvBucket)
		return e
	}); err != nil {
		_ = db.Close() // best-effort cleanup; the bucket-create error is what we report
		return nil, err
	}
	return &Bolt{db: db}, nil
}

// Put implements mantlekeep.Store.
func (b *Bolt) Put(_ context.Context, key string, value []byte) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(kvBucket).Put([]byte(key), value)
	})
}

// Get implements mantlekeep.Store.
func (b *Bolt) Get(_ context.Context, key string) ([]byte, error) {
	var out []byte
	err := b.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(kvBucket).Get([]byte(key))
		if v == nil {
			return fmt.Errorf("key %q not found", key)
		}
		out = append([]byte(nil), v...)
		return nil
	})
	return out, err
}

// List implements mantlekeep.Store.
func (b *Bolt) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	err := b.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(kvBucket).Cursor()
		for k, _ := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			keys = append(keys, string(k))
		}
		return nil
	})
	return keys, err
}

// Close releases the bbolt file.
func (b *Bolt) Close() error { return b.db.Close() }
