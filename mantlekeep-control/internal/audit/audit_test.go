package audit

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	bolt "go.etcd.io/bbolt"
)

// open returns a Bolt log on a fresh temp database, closed when the test ends.
func open(t *testing.T) *Bolt {
	t.Helper()
	b, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// log appends a record for intentID and returns what was stored.
func log(t *testing.T, b *Bolt, intentID string) mantlekeep.AuditRecord {
	t.Helper()
	rec, err := b.Log(context.Background(), mantlekeep.AuditRecord{
		IntentID: intentID, SubjectID: "alice", Action: "job.run",
		Decision: mantlekeep.ActionAllow,
	})
	if err != nil {
		t.Fatalf("Log(%s): %v", intentID, err)
	}
	return rec
}

// The chain link is the whole evidence claim: each record's PrevHash must be the
// previous record's Hash, and every record must carry a hash of its own.
func TestLogChainsEachRecordToTheOneBefore(t *testing.T) {
	b := open(t)

	first := log(t, b, "INT-001")
	if first.Hash == "" {
		t.Fatal("first record stored with no hash — nothing later can chain to it")
	}
	if first.PrevHash != "" {
		t.Errorf("first record PrevHash = %q, want empty (it starts the chain)", first.PrevHash)
	}

	second := log(t, b, "INT-002")
	if second.PrevHash != first.Hash {
		t.Errorf("second record PrevHash = %q, want the first record's hash %q",
			second.PrevHash, first.Hash)
	}
	if second.Hash == first.Hash {
		t.Error("two different records hashed identically — the hash does not cover the content")
	}
}

func TestVerifyReportsAnUntouchedChainIntact(t *testing.T) {
	b := open(t)
	log(t, b, "INT-001")
	log(t, b, "INT-002")
	log(t, b, "INT-003")

	intact, err := b.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !intact {
		t.Error("an untouched chain verified as broken")
	}
}

// Tampering is the case the whole package exists for: rewrite a stored record behind
// the logger's back and Verify must refuse to call the chain intact.
func TestVerifyDetectsAnEditedRecord(t *testing.T) {
	b := open(t)
	log(t, b, "INT-001")
	log(t, b, "INT-002")

	// Rewrite the FIRST record's action, leaving its stored Hash alone, exactly as an
	// attacker editing the database would.
	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucket)
		k, v := bkt.Cursor().First()
		var rec mantlekeep.AuditRecord
		if e := json.Unmarshal(v, &rec); e != nil {
			return e
		}
		rec.Action = "job.delete" // the record now lies about what was decided
		edited, e := json.Marshal(rec)
		if e != nil {
			return e
		}
		return bkt.Put(k, edited)
	})
	if err != nil {
		t.Fatalf("tamper: %v", err)
	}

	intact, err := b.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if intact {
		t.Error("an edited record verified as intact — tampering is undetectable")
	}
}

// Verify walks the chain by unmarshalling each record; a value that is not a record
// at all must break the walk rather than be skipped over as if it were not there.
func TestVerifyDetectsAnUnreadableRecord(t *testing.T) {
	b := open(t)
	log(t, b, "INT-001")

	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucket)
		k, _ := bkt.Cursor().First()
		return bkt.Put(k, []byte("not json"))
	})
	if err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	intact, err := b.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if intact {
		t.Error("an unreadable record verified as intact")
	}
}

// Count is the companion evidence to Verify: an EMPTY chain also verifies as intact,
// so the count is what proves the records survived.
func TestCountReportsWhatSurvived(t *testing.T) {
	b := open(t)

	n, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("empty chain Count = %d, want 0", n)
	}

	log(t, b, "INT-001")
	log(t, b, "INT-002")

	if n, err = b.Count(context.Background()); err != nil {
		t.Fatalf("Count: %v", err)
	} else if n != 2 {
		t.Errorf("Count = %d, want 2", n)
	}
}

func TestRecordsReturnsMostRecentFirstAndHonoursLimit(t *testing.T) {
	b := open(t)
	log(t, b, "INT-001")
	log(t, b, "INT-002")
	log(t, b, "INT-003")

	all, err := b.Records(context.Background(), 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	want := []string{"INT-003", "INT-002", "INT-001"}
	if len(all) != len(want) {
		t.Fatalf("Records(0) returned %d records, want %d", len(all), len(want))
	}
	for i, id := range want {
		if all[i].IntentID != id {
			t.Errorf("Records(0)[%d] = %s, want %s (most recent first)", i, all[i].IntentID, id)
		}
	}

	limited, err := b.Records(context.Background(), 2)
	if err != nil {
		t.Fatalf("Records(2): %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("Records(2) returned %d records, want 2", len(limited))
	}
	if limited[0].IntentID != "INT-003" {
		t.Errorf("Records(2)[0] = %s, want INT-003", limited[0].IntentID)
	}
}
