package serve

import (
	"log/slog"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// embeddedDoorLabel is what the startup banner says the door is when none is dialled.
const embeddedDoorLabel = "in-process"

// doorFor chooses the door this estate submits through: the one the binary supplied, or the one
// dial builds.
//
// dial is a function rather than a client so that an EMBEDDED door never causes a dialled one to
// be built — nothing reads the door URL, and nothing can later mistake a half-configured HTTP
// client for the door in force.
//
// Extracted from [Run] for the same reason [managerFor] was: the failure worth catching is an
// option that exists, is documented, and is never read. A deployment that embeds the door and is
// silently dialled anyway is governed by a service it believes it does not run.
func doorFor(options Options, dial func() mantlekeep.Submitter) mantlekeep.Submitter {
	if options.Door != nil {
		slog.Info("the door is embedded in this process, so -door is not dialled — an embedded " +
			"Submitter must record the delegation itself")
		return options.Door
	}
	return dial()
}

// doorLabel is how the startup banner names the door in force. A banner that printed the -door
// URL for an embedded door would send an operator looking for a service this topology does not
// have.
func doorLabel(options Options, doorURL string) string {
	if options.Door != nil {
		return embeddedDoorLabel
	}
	return doorURL
}
