package estate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const litLabelsMarshalV = "marshal: %v"
const litLabelsResolveV = "resolve: %v"

// labelledManifest is the fixture these tests declare against. Deliberately anonymous: this
// module knows no organisation's structure, and a fixture that borrowed one would teach the
// vocabulary back to every reader of the tests.
func labelledManifest(t *testing.T, document string) Manifest {
	t.Helper()
	manifest, err := ParseManifest([]byte(document))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return manifest
}

func labelPlacer() *Placer {
	return NewPlacer([]Cluster{{
		Name: "dev-app-alpha-1", Provider: "internal", Region: "alpha",
		Env: "dev", Purpose: "app", Residency: "alpha", Reachable: true,
	}})
}

// A label may not take the name of a field the engine governs.
//
// Driven from the DERIVED set rather than from a list written here, so this test covers a field
// added to [Manifest] or [DesiredItem] tomorrow with no edit of its own. That is the whole
// argument for deriving: a hand-written list and a hand-written test agree with each other
// forever and with the engine only until somebody adds a field.
func TestALabelMayNotTakeTheNameOfAGovernedField(t *testing.T) {
	checked := 0
	for governed := range governedFieldNames {
		// A name the label grammar cannot express is unreachable as a key, so refusing it
		// proves nothing. Only the ones a team could actually write are evidence.
		if !name.MatchString(governed) {
			continue
		}
		checked++
		document := `{"team":"team-a","owns":"team-a","labels":{"` + governed + `":"alpha"}}`
		_, err := ParseManifest([]byte(document))
		if err == nil {
			t.Fatalf("label %q was accepted, and it is the name of a field this engine "+
				"governs — it will render beside the real value and govern nothing", governed)
		}
		if !strings.Contains(err.Error(), "governs") {
			t.Fatalf("label %q was refused for the wrong reason: %v", governed, err)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d governed names were reachable as label keys — the derivation is "+
			"returning almost nothing and this test would pass on an empty set", checked)
	}
}

// The forbidden set is DERIVED, not enumerated: every JSON field name on the engine's own
// shapes is in it, and one nested inside a section is too.
//
// The second half is the part a list gets wrong. A name is confusing wherever it is governed,
// and a list written from the top-level fields protects the outer layer only.
// assertEveryDeclaredNameIsGoverned checks one shape's JSON names against the derived set.
//
// Extracted so the test above reads as the three properties it asserts — every declared name,
// every nested name, and the mechanism itself — rather than as a loop with the claim buried
// three levels in.
func assertEveryDeclaredNameIsGoverned(t *testing.T, shape reflect.Type) {
	t.Helper()
	for index := range shape.NumField() {
		field := shape.Field(index)
		declared, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if declared == "" || declared == "-" {
			continue
		}
		if !governsFieldNamed(declared) {
			t.Fatalf("%s.%s is parsed as %q and is not in the governed set — the "+
				"derivation has fallen behind the shape it derives from",
				shape.Name(), field.Name, declared)
		}
	}
}

func TestTheForbiddenSetIsDerivedFromTheEnginesOwnShapes(t *testing.T) {
	for _, shape := range []reflect.Type{reflect.TypeOf(Manifest{}), reflect.TypeOf(DesiredItem{})} {
		assertEveryDeclaredNameIsGoverned(t, shape)
	}

	// Reachable, not top-level: a field on a nested section is read by the engine exactly as an
	// outer one is.
	for _, nested := range []string{"residency", "database", "considered"} {
		if !governsFieldNamed(nested) {
			t.Fatalf("%q is a field on a nested section and is not governed vocabulary — the "+
				"walk stopped at the top level", nested)
		}
	}

	// And the mechanism itself: a name that exists only on a type declared HERE is picked up
	// with nothing edited anywhere. This is what "derived" means, stated as a property.
	type inner struct {
		Second string `json:"secondname"`
	}
	type outer struct {
		First   string `json:"firstname"`
		Nested  *inner `json:"nested"`
		Ignored string `json:"-"`
		Bare    string
	}
	derived := fieldNamesReachableFrom(reflect.TypeOf(outer{}))
	for _, expected := range []string{"firstname", "nested", "secondname"} {
		if _, found := derived[expected]; !found {
			t.Fatalf("%q was not derived from the shape that declares it: %v", expected, derived)
		}
	}
	for _, absent := range []string{"-", "ignored", "bare"} {
		if _, found := derived[absent]; found {
			t.Fatalf("%q is not vocabulary anybody writes and must not be reserved", absent)
		}
	}
}

// An app may ADD a label its team never declared, and may not restate one its team did.
func TestAnAppMayAddALabelButNeverRedefineItsTeams(t *testing.T) {
	added := labelledManifest(t, `{"team":"team-a","owns":"team-a",
	    "labels":{"stream":"alpha"},
	    "apps":[{"name":"svc-one","runtime":"enterprise","image":"registry/team-a/svc-one",
	             "placement":{"env":"dev","purpose":"app","residency":"alpha"},
	             "labels":{"surface":"beta"}}]}`)
	if got := added.labelsFor(added.Apps[0]); got["stream"] != "alpha" || got["surface"] != "beta" {
		t.Fatalf("an app's effective labels must be its team's plus its own; got %v", got)
	}

	_, err := ParseManifest([]byte(`{"team":"team-a","owns":"team-a",
	    "labels":{"stream":"alpha"},
	    "apps":[{"name":"svc-one","runtime":"enterprise","image":"registry/team-a/svc-one",
	             "placement":{"env":"dev","purpose":"app","residency":"alpha"},
	             "labels":{"stream":"beta"}}]}`))
	if err == nil {
		t.Fatal("an app restated its team's label — the estate would report the app under a " +
			"heading its own team disclaims")
	}
	if !strings.Contains(err.Error(), "redefines") {
		t.Fatalf("the refusal must say what happened; got %v", err)
	}
}

// A label reaches a UI, a log line and an evidence record. It must not be able to carry a
// second line into any of them, nor arbitrary length.
func TestALabelCannotCarryANewlineOrUnboundedText(t *testing.T) {
	for _, testCase := range []struct {
		named string
		key   string
		value string
	}{
		{"a newline forges a second log entry", "stream", "alpha\nbeta"},
		{"a carriage return hides what precedes it", "stream", "alpha\rbeta"},
		{"an escape sequence rewrites a terminal", "stream", "alpha\x1b[2Kbeta"},
		{"a tab breaks a column-separated record", "stream", "alpha\tbeta"},
		{"a delete character is invisible in a report", "stream", "alpha\x7f"},
		{"unbounded text is a payload, not a description", "stream", strings.Repeat("a", maxLabelValue+1)},
		{"an empty value describes nothing and holds the name", "stream", ""},
		{"an upper-case key is not the narrow shape", "Stream", "alpha"},
		{"a key with a slash is not the narrow shape", "a/b", "alpha"},
		{"a key with a space is not the narrow shape", "two words", "alpha"},
	} {
		t.Run(testCase.named, func(t *testing.T) {
			document, err := json.Marshal(map[string]any{
				"team": "team-a", "owns": "team-a",
				"labels": map[string]string{testCase.key: testCase.value},
			})
			if err != nil {
				t.Fatalf(litLabelsMarshalV, err)
			}
			if _, err := ParseManifest(document); err == nil {
				t.Fatalf("accepted %q = %q", testCase.key, testCase.value)
			}
		})
	}

	// And the boundary is a boundary, not a wall: a value exactly at the limit is a
	// description, and refusing it would be a rule nobody could predict.
	atLimit, err := json.Marshal(map[string]any{
		"team": "team-a", "owns": "team-a",
		"labels": map[string]string{"stream": strings.Repeat("a", maxLabelValue)},
	})
	if err != nil {
		t.Fatalf(litLabelsMarshalV, err)
	}
	if _, err := ParseManifest(atLimit); err != nil {
		t.Fatalf("a value exactly at the bound was refused: %v", err)
	}
}

// Labels reach the RESOLVED change, so a consumer groups a resolved estate without rebuilding
// the resolver's naming rule.
func TestLabelsReachTheResolvedChange(t *testing.T) {
	manifest := labelledManifest(t, `{"team":"team-a","owns":"team-a",
	    "labels":{"stream":"alpha"},
	    "kafka":{"cluster":"bus-one","topics":["one"]},
	    "apps":[{"name":"svc-one","runtime":"enterprise","image":"registry/team-a/svc-one",
	             "placement":{"env":"dev","purpose":"app","residency":"alpha"},
	             "labels":{"surface":"beta"}}]}`)

	desired, err := ResolveWith(manifest, DefaultFloor(), labelPlacer(), nil)
	if err != nil {
		t.Fatalf(litLabelsResolveV, err)
	}
	if len(desired.Changes) < 3 {
		t.Fatalf("expected the boundary, the topic and the app; got %d", len(desired.Changes))
	}

	sawApp := false
	for _, change := range desired.Changes {
		if change.Labels["stream"] != "alpha" {
			t.Fatalf("%s/%s reached the consumer without the team's label: %v",
				change.Asset, change.Name, change.Labels)
		}
		if change.Asset != "app" {
			if _, leaked := change.Labels["surface"]; leaked {
				t.Fatalf("%s/%s carries one app's label — labels inherit downward, never sideways",
					change.Asset, change.Name)
			}
			continue
		}
		sawApp = true
		if change.Labels["surface"] != "beta" {
			t.Fatalf("the app change lost its own label: %v", change.Labels)
		}
	}
	if !sawApp {
		t.Fatal("no app change was resolved, so nothing about app labels was checked")
	}
}

// Every change gets its OWN map. A consumer that annotates one change must not silently
// annotate every other change resolved from the same manifest.
func TestOneChangesLabelsAreNotEveryChangesLabels(t *testing.T) {
	manifest := labelledManifest(t, `{"team":"team-a","owns":"team-a",
	    "labels":{"stream":"alpha"},
	    "kafka":{"cluster":"bus-one","topics":["one","two"]}}`)

	desired, err := Resolve(manifest, DefaultFloor())
	if err != nil {
		t.Fatalf(litLabelsResolveV, err)
	}
	desired.Changes[0].Labels["stream"] = "mutated"
	for _, change := range desired.Changes[1:] {
		if change.Labels["stream"] != "alpha" {
			t.Fatalf("%s was changed by writing to another change's labels — they share a map",
				change.Name)
		}
	}
}

// An unlabelled manifest resolves to exactly the document it always did. A released module
// gains a field; nothing that never uses it may notice.
func TestAnUnlabelledManifestResolvesWithNoLabelsField(t *testing.T) {
	manifest := labelledManifest(t, `{"team":"team-a","owns":"team-a",
	    "kafka":{"cluster":"bus-one","topics":["one"]}}`)

	desired, err := Resolve(manifest, DefaultFloor())
	if err != nil {
		t.Fatalf(litLabelsResolveV, err)
	}
	document, err := json.Marshal(desired)
	if err != nil {
		t.Fatalf(litLabelsMarshalV, err)
	}
	if strings.Contains(string(document), "labels") {
		t.Fatalf("a manifest that declares no labels resolved to a document mentioning them: %s",
			document)
	}
}
