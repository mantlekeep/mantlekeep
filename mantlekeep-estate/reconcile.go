package estate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Observed is what actually exists right now, as read back from the assets themselves.
//
// Never what we asked for. An adapter that reports its own request is testimony; an adapter
// that reads the cluster is evidence, and only the second can detect that somebody changed
// something by hand.
type Observed struct {
	Items []ObservedItem `json:"items"`
}

// ObservedItem is one resource as it actually is.
type ObservedItem struct {
	Asset   string `json:"asset"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Cluster string `json:"cluster,omitempty"`
	Slot    Slot   `json:"slot,omitempty"`
	Limits  any    `json:"limits"`
	// Readers is who was found to hold read access, as read back from the asset. It mirrors
	// DesiredItem.Readers so a cross-team read granted BY HAND is at least visible in the
	// evidence — an ungoverned path to another team's data is the highest-consequence hand
	// edit there is. [differs] does not compare it yet: a reader is a list rather than a
	// field, and comparing lists needs its own ownership entry and its own decision about
	// whether removing one is a correction or an outage.
	Readers []string `json:"readers,omitempty"`
	Image   string   `json:"image,omitempty"`

	// Writers is who last wrote each field, as the backend itself recorded it — not as we
	// inferred it.
	//
	// This is the difference between "the config drifted" and "this field was changed by
	// this tool at this time". A drift report that only says something differs invites an
	// argument; one that names the writer and the moment is a record the backend produced,
	// which nobody in the argument wrote.
	//
	// It is deliberately NOT a person. Kubernetes records a field MANAGER — a tool name
	// like "kubectl-patch" — because that is what the API server can attest to. The human
	// behind it lives in the API server's audit log, under a different access grant. Naming
	// a person here would be inventing evidence the backend did not give us.
	Writers []Writer `json:"writers,omitempty"`
	Digest  string   `json:"digest,omitempty"`
	Runtime string   `json:"runtime,omitempty"`
	// State is what the asset's own fields actually are, keyed as in [DesiredItem.State]. An
	// adapter that reads a cluster fills this; one that reads a deployment need not.
	State map[string]string `json:"state,omitempty"`
}

// Writer is one backend-attested claim over a set of fields.
//
// Every field is the backend's own answer, copied rather than derived: Manager is what the
// writer called itself, Operation is how it wrote, At is when the backend recorded it, and
// Fields are the paths that claim covers. Deriving any of them would make this our account
// of what happened rather than the backend's.
type Writer struct {
	// Manager is the tool that wrote, e.g. "mantlekeep-estate", "kubectl-patch", "helm".
	Manager string `json:"manager"`
	// Operation is how it wrote — an apply that declares ownership, or a bare update.
	Operation string `json:"operation,omitempty"`
	// At is when the backend recorded the claim.
	At time.Time `json:"at,omitempty"`
	// Fields are the paths this writer owns, as the backend reports them.
	Fields []string `json:"fields,omitempty"`
}

func (o ObservedItem) key() string {
	if !o.Slot.Empty() {
		return o.Asset + "/" + o.Kind + "/" + o.Slot.Key()
	}
	return o.Asset + "/" + o.Kind + "/" + o.Name
}

func (o Observed) index() map[string]ObservedItem {
	byKey := make(map[string]ObservedItem, len(o.Items))
	for _, item := range o.Items {
		byKey[item.key()] = item
	}
	return byKey
}

// key is the identity two states are compared on. It includes the SLOT: without it, the same
// app deployed to two namespaces during a changeover collides, and the reconciler treats one
// live version as drift from the other.
func (d DesiredItem) key() string {
	if !d.Slot.Empty() {
		return d.Asset + "/" + d.Kind + "/" + d.Slot.Key()
	}
	return d.Asset + "/" + d.Kind + "/" + d.Name
}

// DriftKind says HOW reality differs from what was approved. They are separated because they
// call for different answers: absent work must be created, changed work must be corrected or
// escalated, and unexpected work is the one nobody plans for and the one that matters most.
type DriftKind string

const (
	// DriftAbsent — approved, but not there. The ordinary case on first apply.
	DriftAbsent DriftKind = "absent"
	// DriftChanged — there, but not as approved. Someone or something altered it.
	DriftChanged DriftKind = "changed"
	// DriftUnexpected — exists, was never approved. A resource nobody signed off is either a
	// bypass of the platform or a leftover, and both need a human to look. It is NEVER deleted
	// automatically: deleting data because it is unrecognised turns a governance gap into an
	// outage.
	DriftUnexpected DriftKind = "unexpected"
)

// Drift is one divergence between approved state and reality.
type Drift struct {
	Kind     DriftKind     `json:"kind"`
	Desired  *DesiredItem  `json:"desired,omitempty"`
	Observed *ObservedItem `json:"observed,omitempty"`
	// Detail says what differs, in words a human reads before deciding.
	Detail string `json:"detail,omitempty"`
	// Differences names each field that diverged, so a person can see WHAT changed rather
	// than only that something did.
	Differences []Difference `json:"differences,omitempty"`
}

// Ungoverned reports drift MantleKeep must act on: an unapproved resource, or a difference in a
// field it owns. A difference only in WATCHED fields is a fact about the platform doing its
// job, not a violation.
func (d Drift) Ungoverned() bool {
	if d.Kind == DriftUnexpected || d.Kind == DriftAbsent {
		return true
	}
	for _, difference := range d.Differences {
		if difference.Governed {
			return true
		}
	}
	return false
}

// Correctable reports whether this drift may be closed by the reconciler on its own.
//
// Only where the approved change carried NO gate. A resource that needed a human to exist
// needs a human to change, and a reconciler that quietly re-applies a gated resource is making
// an ungoverned change in the direction of the last approval — which is still an ungoverned
// change. Unexpected resources are never correctable: nobody approved their existence, so
// there is no approval to act under.
func (d Drift) Correctable() bool {
	if d.Kind == DriftUnexpected || d.Desired == nil {
		return false
	}
	// A difference only in WATCHED fields is the platform doing its job. Correcting it would
	// fight the platform forever, each seeing the other's value as drift.
	if !d.Ungoverned() {
		return false
	}
	return d.Desired.Gate == GateNone
}

// Difference is one field that does not match, and whose problem it is.
type Difference struct {
	Field    string `json:"field"`
	Approved string `json:"approved"`
	Observed string `json:"observed"`
	// Governed marks a field MantleKeep owns. A watched field is reported and never corrected —
	// the platform owns it, and correcting it would fight the platform on a timer.
	Governed bool `json:"governed"`
}

// Diff compares approved desired state against observed reality.
//
// Deterministic order: a drift report that reshuffles between runs reads as churn, and nobody
// reviews a report they cannot compare with yesterday's.
func Diff(desired Desired, observed Observed) []Drift {
	return DiffOwned(desired, observed, DefaultOwnership())
}

// DiffOwned compares two states, judging each field by who owns it.
func DiffOwned(desired Desired, observed Observed, ownership Ownership) []Drift {
	var drifts []Drift
	seen := make(map[string]bool, len(desired.Changes))
	actual := observed.index()

	for _, want := range desired.Changes {
		want := want
		seen[want.key()] = true
		got, exists := actual[want.key()]
		if !exists {
			drifts = append(drifts, Drift{Kind: DriftAbsent, Desired: &want,
				Detail: "approved but absent"})
			continue
		}
		if differences := differs(want, got, ownership); len(differences) > 0 {
			got := got
			drifts = append(drifts, Drift{Kind: DriftChanged, Desired: &want, Observed: &got,
				Detail: summarise(differences), Differences: differences})
		}
	}

	for _, got := range observed.Items {
		got := got
		if !seen[got.key()] {
			drifts = append(drifts, Drift{Kind: DriftUnexpected, Observed: &got,
				Detail: "exists but was never approved"})
		}
	}

	sort.SliceStable(drifts, func(i, j int) bool { return driftKey(drifts[i]) < driftKey(drifts[j]) })
	return drifts
}

func driftKey(d Drift) string {
	if d.Desired != nil {
		return d.Desired.key()
	}
	return d.Observed.key()
}

// differs lists every field that diverged, marking each with whether MantleKeep owns it.
//
// A field neither governed nor watched is skipped entirely: reporting a difference nobody has
// declared an interest in is noise, and noise is what gets a report muted.
func differs(want DesiredItem, got ObservedItem, ownership Ownership) []Difference {
	var differences []Difference
	compare := func(field, approved, observed string) {
		if approved == "" || approved == observed {
			// Nothing was approved for this field, so there is nothing to hold reality to.
			return
		}
		// An EMPTY observed value is not "nothing to compare" — it is the value being GONE.
		// Skipping it made the most dangerous edit invisible: strip the @sha256 pin off a
		// Deployment and the reconciler saw no drift at all, which is precisely the moving
		// pointer an approved digest exists to rule out.
		if observed == "" {
			observed = "(absent)"
		}
		governed := ownership.Owns(field)
		if !governed && !ownership.Watches(field) {
			return
		}
		differences = append(differences, Difference{
			Field: field, Approved: approved, Observed: observed, Governed: governed,
		})
	}

	compare("digest", want.Digest, got.Digest)
	compare("image", want.Image, got.Image)
	compare("runtime", want.Runtime, got.Runtime)

	// The fleet's fields are named beside the fleet model, but they are judged HERE, by the
	// same ownership rules — a differ with one set of rules for apps and another for clusters
	// is two governance models wearing one name.
	fleetDifferences(want, got, compare)

	// Every asset's floor, not just apps. A limit that is applied once and never checked again
	// is a limit that quietly stops holding: somebody widens a retention or a connection limit
	// by hand, and the only record says what was approved a year ago.
	switch wantLimits := want.Limits.(type) {
	case AppLimits:
		if gotLimits, ok := got.Limits.(AppLimits); ok {
			compare("replicas", fmt.Sprint(wantLimits.Replicas), fmt.Sprint(gotLimits.Replicas))
			compare("cpuLimit", wantLimits.CPULimit, gotLimits.CPULimit)
			compare("memoryMiB", fmt.Sprint(wantLimits.MemoryMiB), fmt.Sprint(gotLimits.MemoryMiB))
		}
	case KafkaLimits:
		if gotLimits, ok := got.Limits.(KafkaLimits); ok {
			compare("retention", wantLimits.Retention.String(), gotLimits.Retention.String())
			compare("producerBytesPerSec", fmt.Sprint(wantLimits.ProducerBytesPerSec),
				fmt.Sprint(gotLimits.ProducerBytesPerSec))
			compare("consumerBytesPerSec", fmt.Sprint(wantLimits.ConsumerBytesPerSec),
				fmt.Sprint(gotLimits.ConsumerBytesPerSec))
		}
	case PostgresLimits:
		if gotLimits, ok := got.Limits.(PostgresLimits); ok {
			compare("connectionLimit", fmt.Sprint(wantLimits.ConnectionLimit),
				fmt.Sprint(gotLimits.ConnectionLimit))
			compare("statementTimeout", wantLimits.StatementTimeout.String(),
				gotLimits.StatementTimeout.String())
		}
	case HarborLimits:
		if gotLimits, ok := got.Limits.(HarborLimits); ok {
			compare("robotExpiry", wantLimits.RobotExpiry.String(), gotLimits.RobotExpiry.String())
		}
	}
	return differences
}

func summarise(differences []Difference) string {
	parts := make([]string, 0, len(differences))
	for _, difference := range differences {
		owner := "watched"
		if difference.Governed {
			owner = "governed"
		}
		parts = append(parts, fmt.Sprintf("%s is %q, approved %q (%s)",
			difference.Field, difference.Observed, difference.Approved, owner))
	}
	return strings.Join(parts, "; ")
}

// Port is one asset's adapter. Kafka, Postgres, Harbor and the app runtime each implement it; this
// package never learns what any of them is.
type Port interface {
	// Asset names what this adapter provisions, e.g. "kafka".
	Asset() string
	// Observe reads back what actually exists for a team. The reconciler's eyes, and the same
	// call that produces evidence after an apply.
	Observe(ctx context.Context, team string) (Observed, error)
	// Apply makes one change, under a token the door already issued. An adapter must refuse a
	// token it was not given, or one that has expired, rather than act unauthenticated.
	//
	// The WHOLE token, not its Value. An adapter needs two different things from it and they
	// must not be confused: Value is the opaque signed CAPABILITY it authorises with, and
	// IntentID is the chain reference it records on whatever it creates. Pass only the Value
	// and the sole string an adapter can record is the one that must stay secret — which is
	// how a live capability ends up written onto an object anyone can read.
	Apply(ctx context.Context, token mantlekeep.ExecutionToken, change DesiredItem) error
}

// Outcome is what one reconcile pass did and, more usefully, what it refused to do.
type Outcome struct {
	Corrected []Drift `json:"corrected"`
	// Escalated is drift a human must rule on: anything gated, and anything unexpected.
	Escalated []Drift `json:"escalated"`
	Failed    []Drift `json:"failed"`
}

// Reconcile closes the gap between approved state and reality, once.
//
// It corrects only what nobody had to approve, and escalates the rest. That split is what
// separates this from a plain declarative reconciler: reconciling to whatever the declared
// state says carries no notion that some changes needed a person. Here a gated resource that
// has drifted becomes a decision, not an automatic action.
func Reconcile(ctx context.Context, port Port, token mantlekeep.ExecutionToken, drifts []Drift) Outcome {
	var outcome Outcome
	for _, drift := range drifts {
		if !drift.Correctable() {
			outcome.Escalated = append(outcome.Escalated, drift)
			continue
		}
		if err := port.Apply(ctx, token, *drift.Desired); err != nil {
			drift.Detail = fmt.Sprintf("%s; correction failed: %v", drift.Detail, err)
			outcome.Failed = append(outcome.Failed, drift)
			continue
		}
		outcome.Corrected = append(outcome.Corrected, drift)
	}
	return outcome
}
