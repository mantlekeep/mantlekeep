package estate

import (
	"reflect"
	"strings"
)

// governedFieldNames is every field name the engine itself reads or writes, derived from the
// declared and resolved shapes rather than listed.
//
// Derived, not enumerated, for two reasons. A hand-written list is a SECOND place to edit: add
// a field to [Manifest] and the list still describes yesterday's engine, so the new field is
// governed and unprotected at the same time. And a list of names is a disclosure — it says out
// loud which assets one deployment happens to run, in a module whose whole claim is that it
// knows no product, no environment and no tool.
//
// Reflection over the JSON tags is the same source the decoder uses, so the set cannot disagree
// with what is actually parsed. A field with no tag, or `json:"-"`, is not part of the
// vocabulary anybody writes, so it contributes nothing.
var governedFieldNames = fieldNamesReachableFrom(
	reflect.TypeOf(Manifest{}),
	reflect.TypeOf(DesiredItem{}),
)

// fieldNamesReachableFrom collects the JSON field names of each root and of every struct
// reachable from it.
//
// Reachable rather than top-level: a name is confusing wherever it is governed. A field on a
// nested section is read by the engine exactly as a top-level one is, so protecting only the
// outer layer would leave the inner vocabulary free to be shadowed.
//
// Names are folded to lower case because that is how they are compared. A name that differs
// from a governed field only in case is not a different word to the person reading the report.
func fieldNamesReachableFrom(roots ...reflect.Type) map[string]struct{} {
	names := make(map[string]struct{})
	visited := make(map[reflect.Type]bool)
	for _, root := range roots {
		collectFieldNames(root, names, visited)
	}
	return names
}

// collectFieldNames walks one type. `visited` guards against a shape that refers to itself —
// which nothing here does today, and which must not turn into a hang the day one does.
func collectFieldNames(shape reflect.Type, names map[string]struct{}, visited map[reflect.Type]bool) {
	shape = elementOf(shape)
	if shape.Kind() != reflect.Struct || visited[shape] {
		return
	}
	visited[shape] = true

	for index := range shape.NumField() {
		field := shape.Field(index)
		if declared, _, _ := strings.Cut(field.Tag.Get("json"), ","); declared != "" && declared != "-" {
			names[strings.ToLower(declared)] = struct{}{}
		}
		collectFieldNames(field.Type, names, visited)
	}
}

// elementOf unwraps the containers a field can be declared through, so a section held as a
// pointer or a slice is still walked. A map is NOT unwrapped: its keys are values a caller
// chooses, not names the engine declares, so nothing inside one is governed vocabulary.
func elementOf(shape reflect.Type) reflect.Type {
	for shape.Kind() == reflect.Pointer || shape.Kind() == reflect.Slice ||
		shape.Kind() == reflect.Array {
		shape = shape.Elem()
	}
	return shape
}

// governsFieldNamed reports whether the engine already reads a field by this name.
func governsFieldNamed(candidate string) bool {
	_, taken := governedFieldNames[strings.ToLower(candidate)]
	return taken
}
