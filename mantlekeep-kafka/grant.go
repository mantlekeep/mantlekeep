package kafkagrant

import (
	"fmt"
	"strings"
)

// Principal is a Kafka principal in its wire form, "<type>:<name>" — conventionally
// "User:svc-payments". Both halves matter and they are used in different places: an ACL
// binding carries the whole string, while a client quota entity carries only the NAME.
// Keeping it one type stops a caller passing a bare name where a principal belongs.
type Principal string

// Type returns the principal type ("User"), and Name the identity within it
// ("svc-payments").
func (p Principal) Type() string { typ, _, _ := p.split(); return typ }

// Name returns the identity within the principal type — the value a client quota
// entity component is keyed on.
func (p Principal) Name() string { _, name, _ := p.split(); return name }

// Validate reports whether the principal has the "<type>:<name>" shape Kafka requires,
// with both halves non-empty.
func (p Principal) Validate() error {
	if _, _, ok := p.split(); !ok {
		return fmt.Errorf("%w: principal %q must be \"<type>:<name>\", e.g. \"User:svc-payments\"", ErrInvalidGrant, string(p))
	}
	return nil
}

func (p Principal) split() (typ, name string, ok bool) {
	typ, name, found := strings.Cut(string(p), ":")
	if !found || typ == "" || name == "" {
		return "", "", false
	}
	return typ, name, true
}

// Quota is the resource floor for one principal, in bytes per second.
//
// It is not optional and it is not defaulted. Without a quota a single team can
// saturate the brokers for everyone — the accidental denial-of-service this exists to
// prevent — and a quota the adapter invented would be a limit nobody approved. Both
// rates are supplied by the caller, who owns that decision.
type Quota struct {
	ProducerByteRate int64 // bytes/sec the principal may produce
	ConsumerByteRate int64 // bytes/sec the principal may consume
}

// Validate rejects a quota that would not actually bound anything.
func (q Quota) Validate() error {
	if q.ProducerByteRate <= 0 || q.ConsumerByteRate <= 0 {
		return fmt.Errorf("%w: quota must set a positive producer and consumer byte rate (got %d/%d) — "+
			"an unbounded principal can starve the cluster, and this adapter will not choose the bound for you",
			ErrInvalidGrant, q.ProducerByteRate, q.ConsumerByteRate)
	}
	return nil
}

// Boundary is the namespace a team owns: one principal, one prefix, one quota.
//
// This is the RARE, gated act. Everything the team later does without a fresh approval
// is bounded by what this value says.
type Boundary struct {
	Principal Principal // who the namespace belongs to, e.g. "User:svc-payments"
	Prefix    string    // the topic AND consumer-group prefix the team owns, e.g. "payments."
	Quota     Quota     // the resource floor for Principal — caller-supplied, never defaulted
}

// Validate checks the boundary is well formed. It does NOT check whether the prefix is
// an acceptable one to hand out: which prefixes a team may own is a policy question the
// door already answered before this package was reached.
func (b Boundary) Validate() error {
	if err := b.Principal.Validate(); err != nil {
		return err
	}
	if err := validatePrefix(b.Prefix); err != nil {
		return err
	}
	return b.Quota.Validate()
}

// Grant is one topic to create inside a boundary the team ALREADY owns.
//
// This is the FREQUENT, instant act. No permission is granted by it — the PREFIXED ACL
// written at onboarding already covers any name under Prefix. The only question left is
// whether Topic really is under Prefix, which is why that is the one thing refused here.
type Grant struct {
	Principal         Principal // the owning principal, carried for evidence and for the boundary check
	Prefix            string    // the prefix the team was onboarded with — the boundary Topic must fall inside
	Topic             string    // the topic to create, e.g. "payments.settlement.v1"
	Partitions        int32     // caller-supplied; -1 asks the broker for its default (Kafka 2.4+)
	ReplicationFactor int16     // caller-supplied; -1 asks the broker for its default (Kafka 2.4+)
	RetentionMillis   int64     // the retention floor to apply at creation; 0 leaves the broker default
}

// Validate checks the grant is well formed and, critically, that Topic falls inside
// Prefix. A resource outside the boundary is refused with [ErrOutsideBoundary].
func (g Grant) Validate() error {
	if err := g.Principal.Validate(); err != nil {
		return err
	}
	if err := validatePrefix(g.Prefix); err != nil {
		return err
	}
	if err := validateResourceName(g.Topic, "topic"); err != nil {
		return err
	}
	if g.RetentionMillis < 0 {
		return fmt.Errorf("%w: retention must not be negative (got %d)", ErrInvalidGrant, g.RetentionMillis)
	}
	if !covers(g.Prefix, g.Topic) {
		return fmt.Errorf("%w: topic %q is outside the granted prefix %q — "+
			"creating it would use a permission nobody approved", ErrOutsideBoundary, g.Topic, g.Prefix)
	}
	return nil
}
