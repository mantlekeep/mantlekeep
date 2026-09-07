package estate

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// Labels is descriptive vocabulary a deployment attaches to what it declares, so it can group
// its own estate by words this module has never heard of.
//
// # Descriptive, and only descriptive
//
// Nothing reads a label to make a decision. Not the floor, not the gate, not placement, not the
// differ — a label travels to a report, a log line and an evidence record, and stops there. That
// is the whole contract, and it is why a label may be authored freely: a field nobody decides on
// cannot be used to decide anything.
//
// # Why a label may not take a governed field's name
//
// The moment a label is allowed to be called what a governed field is called, it renders beside
// the real value, in the same table, in the same words. It looks authoritative and governs
// nothing — a team believes it has set something it has not, which is the same failure mode
// [ParseManifest] refuses unknown fields for. Worse is the day somebody wires it up: a
// descriptive field that starts being read is a field a team can edit to change an outcome, and
// re-labelling becomes a way around the gate that field was protecting.
//
// So the rule is structural rather than advisory: a label key that names a field the engine
// governs is refused at parse time. The forbidden set is DERIVED from the engine's own shapes
// (see [governedFieldNames]) rather than written down, so it can never fall behind them.
//
// # Bounded on purpose
//
// A label reaches a UI, a log line and an evidence record. Unbounded text in any of those is
// somebody else's problem to render safely, and a newline in a log line is a forged second
// entry. Keys take the same narrow shape as every other name here; values are bounded and
// carry no control characters.
type Labels map[string]string

// maxLabelValue bounds one value. Generous enough for a human-readable phrase, small enough
// that a label cannot become a payload smuggled through the manifest into an evidence record.
const maxLabelValue = 128

// validate checks every key and value, in a stable order.
//
// Sorted rather than in map order: an error message that names a different offending key on
// each run is one a person cannot act on, and one a test cannot assert.
func (l Labels) validate(what string) error {
	for _, key := range l.keys() {
		if !name.MatchString(key) {
			return fmt.Errorf(
				"manifest: %s label %q is not a valid label name — a label is read by people "+
					"and rendered by tools, so it takes the same narrow shape as every other "+
					"name here", what, key)
		}
		if governsFieldNamed(key) {
			return fmt.Errorf(
				"manifest: %s label %q takes the name of a field this engine governs — a label "+
					"is descriptive and nothing reads one to decide, so one wearing a governed "+
					"field's name renders beside the real value, looks authoritative and "+
					"governs nothing", what, key)
		}
		if err := validateLabelValue(what, key, l[key]); err != nil {
			return err
		}
	}
	return nil
}

// validateLabelValue bounds one value.
func validateLabelValue(what, key, value string) error {
	if value == "" {
		return fmt.Errorf(
			"manifest: %s label %q has no value — an empty label describes nothing and still "+
				"occupies the name", what, key)
	}
	if utf8.RuneCountInString(value) > maxLabelValue {
		return fmt.Errorf(
			"manifest: %s label %q is longer than %d characters — a label is a description, "+
				"not a document, and it is carried into records that must stay readable",
			what, key, maxLabelValue)
	}
	for _, character := range value {
		// Control characters, not newlines alone. A newline forges a second log entry; a
		// carriage return hides what precedes it; an escape sequence rewrites a terminal. All
		// three are the same mistake, and enumerating one of them invites the other two.
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf(
				"manifest: %s label %q carries a control character — a label is written into "+
					"logs and evidence records, where one line must stay one line",
				what, key)
		}
	}
	return nil
}

// keys returns the label names in a deterministic order.
func (l Labels) keys() []string {
	keys := make([]string, 0, len(l))
	for key := range l {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// clone returns an independent copy, or nil when there is nothing to copy.
//
// Independent because the same labels are attached to every change resolved from one manifest,
// and a shared map means a consumer that annotates one change silently annotates all of them.
// Nil rather than an empty map so an unlabelled manifest serialises exactly as it did before
// labels existed — `omitempty` drops a nil map, and a resolved state that gained an empty
// object would change every stored document for no reason.
func (l Labels) clone() Labels {
	if len(l) == 0 {
		return nil
	}
	copied := make(Labels, len(l))
	for key, value := range l {
		copied[key] = value
	}
	return copied
}

// labelsFor returns the labels that describe one app: its team's, plus its own.
//
// Inheritance is additive and one-directional. An app may ADD a key its team never declared —
// that is the point, since a team describes its estate broadly and an app knows its own
// specifics. It may not REDEFINE one, and [Manifest.validateLabels] refuses the attempt at
// parse time rather than resolving a winner here: an app that could restate its team's word
// would be reported under a heading its own team disclaims, and the estate would show two
// answers to one question with no way to tell which was authored last.
func (m Manifest) labelsFor(app App) Labels {
	merged := make(Labels, len(m.Labels)+len(app.Labels))
	for key, value := range m.Labels {
		merged[key] = value
	}
	for key, value := range app.Labels {
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// validateLabels checks the manifest's own labels and each app's, including the rule that binds
// them together.
func (m Manifest) validateLabels() error {
	if err := m.Labels.validate("team " + m.Team); err != nil {
		return err
	}
	for _, app := range m.Apps {
		what := "app " + app.Name
		if err := app.Labels.validate(what); err != nil {
			return err
		}
		for _, key := range app.Labels.keys() {
			if _, declared := m.Labels[key]; declared {
				return fmt.Errorf(
					"manifest: %s redefines label %q, which team %q already declared — an app "+
						"may ADD a label, never restate its team's, or the estate reports the "+
						"app under a heading its own team disclaims",
					what, key, m.Team)
			}
		}
	}
	return nil
}
