package estate

import (
	"context"
	"fmt"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Discovery is a record that something changed which no decision covers.
//
// The chain today holds DECISIONS — what was approved. That makes it a record of intentions. A
// governance system also has to answer the question an auditor actually asks: what happened
// that we did NOT approve? Without discoveries, an out-of-band change is invisible, and worse,
// a reconciler that silently corrects one leaves no trace that it ever occurred — the
// correction looks like routine work.
//
// It is deliberately NOT a decision. Nobody approved anything; the platform observed something.
// Recording it under the same action name as an approval would put a fact and a judgement in
// the same shape, and later nobody could tell them apart.
type Discovery struct {
	Slot     Slot      `json:"slot"`
	Kind     DriftKind `json:"kind"`
	Detail   string    `json:"detail"`
	Observed time.Time `json:"observed"`
}

// Recorder writes discoveries where they survive. The audit chain is the obvious
// implementation; a test uses a fake.
type Recorder interface {
	Record(ctx context.Context, discovery Discovery) error
}

// ChainRecorder writes discoveries onto the hash chain, beside the decisions.
//
// Same chain on purpose. A separate log would be a second record that can be lost, rotated or
// quietly diverge from the decisions it must be read against — and "what was approved" and
// "what actually happened" are only useful in the same ordered sequence.
type ChainRecorder struct {
	Audit mantlekeep.AuditLogger
	// Subject is the platform itself. A discovery has no human author: nobody asked for it, the
	// reconciler noticed it. Attributing it to whoever happened to trigger the pass would name
	// the wrong person on a permanent record.
	Subject string
}

// Record appends one discovery to the chain.
func (c ChainRecorder) Record(ctx context.Context, discovery Discovery) error {
	if c.Audit == nil {
		return fmt.Errorf("discovery: no audit logger configured — an unrecorded discovery is " +
			"the same as never having looked")
	}
	subject := c.Subject
	if subject == "" {
		subject = "mantlekeep-reconciler"
	}
	_, err := c.Audit.Log(ctx, mantlekeep.AuditRecord{
		Timestamp: discovery.Observed.UTC(),
		IntentID:  "DISCOVERY-" + discovery.Slot.Key(),
		SubjectID: subject,
		// A distinct action, so a reader can separate what was DECIDED from what was FOUND.
		Action:   "footprint.discovered",
		Decision: mantlekeep.DecisionAction(discovery.Kind),
		PolicyID: "reconciler",
	})
	return err
}
