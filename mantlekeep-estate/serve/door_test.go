package serve

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// The neutral URL a dialled door would be reached at in these tests.
const dialledURL = "http://door.example:8080"

// THE test for this option: a Door supplied in Options is the one used, and no dialled door is
// even built. The failure guarded against is the quiet one — the option is set, the estate dials
// anyway, and the deployment is governed by a service it believes it does not run.
func TestADoorSuppliedInOptionsIsUsedInsteadOfDialling(t *testing.T) {
	embedded := &identityDoor{name: "embedded"}
	dialled := false

	door := doorFor(Options{Door: embedded}, func() mantlekeep.Submitter {
		dialled = true
		return &identityDoor{name: "dialled"}
	})

	if dialled {
		t.Error("a door was supplied in Options and the estate dialled one anyway")
	}
	if door != embedded {
		t.Fatalf("the estate submits through %v, want the supplied door %v", door, embedded)
	}
}

// Nil keeps the previous behaviour exactly: the door is dialled. Nil must never mean ungoverned.
func TestANilDoorIsDialled(t *testing.T) {
	want := &identityDoor{name: "dialled"}

	door := doorFor(Options{}, func() mantlekeep.Submitter { return want })

	if door != want {
		t.Fatalf("with no door supplied the estate submits through %v, want the dialled %v",
			door, want)
	}
}

// The banner must not name a URL nobody dials. An operator reading "door=http://…" for an
// embedded door goes looking for a service this topology does not have.
func TestTheBannerSaysTheDoorIsInProcessWhenOneIsSupplied(t *testing.T) {
	if got := doorLabel(Options{Door: &identityDoor{}}, dialledURL); got != embeddedDoorLabel {
		t.Errorf("an embedded door is announced as %q, want %q", got, embeddedDoorLabel)
	}
	if got := doorLabel(Options{}, dialledURL); got != dialledURL {
		t.Errorf("a dialled door is announced as %q, want its URL %q", got, dialledURL)
	}
}
