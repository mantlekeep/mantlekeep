// Package audit provides a bbolt-backed AuditLogger: append-only and
// hash-chained (each record's Hash covers the previous record's Hash), so any
// tampering breaks the chain. Impl detail today; ClickHouse / S3 WORM later.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

var bucket = []byte("audit")

// Bolt is an append-only hash-chained audit log over bbolt.
type Bolt struct {
	db *bolt.DB
}

// Open creates/opens the audit database at path.
func Open(path string) (*Bolt, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists(bucket)
		return e
	}); err != nil {
		return nil, err
	}
	return &Bolt{db: db}, nil
}

// Close releases the database.
func (b *Bolt) Close() error { return b.db.Close() }

// hashRecord computes SHA-256 over the record with its own Hash field cleared,
// so the digest covers content + PrevHash but not itself.
func hashRecord(rec mantlekeep.AuditRecord) string {
	rec.Hash = ""
	payload, _ := json.Marshal(rec)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Log implements mantlekeep.AuditLogger. It links to the previous record, computes
// this record's hash, and appends it. Returns the stored record (Hash set).
func (b *Bolt) Log(_ context.Context, rec mantlekeep.AuditRecord) (mantlekeep.AuditRecord, error) {
	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucket)

		rec.PrevHash = ""
		if _, last := bkt.Cursor().Last(); last != nil {
			var prev mantlekeep.AuditRecord
			if json.Unmarshal(last, &prev) == nil {
				rec.PrevHash = prev.Hash
			}
		}
		rec.Hash = hashRecord(rec)

		seq, _ := bkt.NextSequence()
		key := []byte(fmt.Sprintf("%016d", seq))
		val, _ := json.Marshal(rec)
		return bkt.Put(key, val)
	})
	return rec, err
}

// Count returns how many records are on the chain. Verify alone is not enough
// evidence after a restart: an EMPTY chain also verifies as "intact" (the walk
// simply finds nothing to break), so a wiped database would falsely look healthy.
// Reporting the count alongside Verify proves the records actually SURVIVED — the
// durable-evidence beat compares the pre-restart and post-restart counts.
func (b *Bolt) Count(_ context.Context) (int, error) {
	n := 0
	err := b.db.View(func(tx *bolt.Tx) error {
		n = tx.Bucket(bucket).Stats().KeyN
		return nil
	})
	return n, err
}

// Records returns up to limit audit entries, MOST RECENT FIRST. It is a pure
// read (a bbolt View transaction), so it can never alter the trail — this is the
// slice the read-only /api/audit endpoint serves so management can SEE every
// recorded decision. Each returned record still carries its Hash + PrevHash, so
// the chain links are visible in the payload. limit <= 0 returns every record.
func (b *Bolt) Records(_ context.Context, limit int) ([]mantlekeep.AuditRecord, error) {
	var out []mantlekeep.AuditRecord
	err := b.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		// Keys are zero-padded sequence numbers, so Last()→Prev() walks newest→oldest.
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var rec mantlekeep.AuditRecord
			if e := json.Unmarshal(v, &rec); e != nil {
				return e
			}
			out = append(out, rec)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		return nil
	})
	return out, err
}

// Verify walks the chain and reports whether it is intact (no tampering).
func (b *Bolt) Verify(_ context.Context) (bool, error) {
	intact := true
	err := b.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		prevHash := ""
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var rec mantlekeep.AuditRecord
			if json.Unmarshal(v, &rec) != nil {
				intact = false
				return nil
			}
			if rec.PrevHash != prevHash || hashRecord(rec) != rec.Hash {
				intact = false
				return nil
			}
			prevHash = rec.Hash
		}
		return nil
	})
	return intact, err
}
