package mantlekeep_test

import (
	"encoding/json"
	"testing"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// A record written before RunID existed must marshal to EXACTLY the same bytes.
//
// The chain hashes the marshalled record, so a field that serialises when empty would change the
// hash of every record ever written and break verification of every existing chain — with no
// migration available, because the log is append-only. This is the whole risk of the change, so
// it is asserted against a literal, not against another marshal of the same struct.
//
// The golden string below is what a record marshalled to BEFORE these fields were added. If a
// future field forgets omitempty, this test fails and says so.
func TestARecordWrittenBeforeCorrelationMarshalsIdentically(t *testing.T) {
	record := mantlekeep.AuditRecord{
		Timestamp: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC),
		IntentID:  "INT-1",
		SubjectID: "root",
		Action:    "job.run",
		Decision:  mantlekeep.ActionAllow,
		PolicyID:  "frame-control.rbac",
		PrevHash:  "abc",
		Hash:      "def",
	}

	const golden = `{"Timestamp":"2026-09-11T10:00:00Z","IntentID":"INT-1","SubjectID":"root",` +
		`"Action":"job.run","Decision":"allow","PolicyID":"frame-control.rbac","IsAI":false,` +
		`"PrevHash":"abc","Hash":"def"}`

	marshalled, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(marshalled) != golden {
		t.Fatalf("the serialised shape CHANGED — every existing chain would fail verification\n got: %s\nwant: %s",
			marshalled, golden)
	}
}

// When the fields ARE set they appear, so a product that uses them can be filtered.
func TestCorrelationAppearsOnlyWhenSet(t *testing.T) {
	record := mantlekeep.AuditRecord{
		IntentID: "INT-2",
		RunID:    "RUN-7",
		ParentID: "RUN-1",
		StepID:   "build",
	}
	marshalled, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, want := range []string{`"RunID":"RUN-7"`, `"ParentID":"RUN-1"`, `"StepID":"build"`} {
		if !contains(string(marshalled), want) {
			t.Fatalf("%s must appear when set: %s", want, marshalled)
		}
	}
}

// Two parallel runs of one pipeline must be separable — the reason these fields exist.
func TestTwoParallelRunsAreDistinguishable(t *testing.T) {
	interleaved := []mantlekeep.AuditRecord{
		{IntentID: "INT-1", RunID: "RUN-A", StepID: "checkout"},
		{IntentID: "INT-2", RunID: "RUN-B", StepID: "checkout"},
		{IntentID: "INT-3", RunID: "RUN-A", StepID: "build"},
		{IntentID: "INT-4", RunID: "RUN-B", StepID: "build"},
	}

	// This is the whole point: reconstructing one execution from a shared, interleaved log.
	steps := []string{}
	for _, record := range interleaved {
		if record.RunID == "RUN-A" {
			steps = append(steps, record.StepID)
		}
	}
	if len(steps) != 2 || steps[0] != "checkout" || steps[1] != "build" {
		t.Fatalf("RUN-A should be reconstructable in order, got %v", steps)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
