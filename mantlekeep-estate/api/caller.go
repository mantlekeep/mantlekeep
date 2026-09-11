package api

import (
	"fmt"
	"net/http"
	"strings"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// CallerResolver resolves WHO is calling, from the call itself.
//
// An interface because the answer is deployment-specific — a header from a trusted gateway
// here, a verified JWT there — and because the one thing that must never vary is where the
// answer comes FROM. It is always the transport. A body-asserted actor is an unauthenticated
// change wearing a name: the request says "this person did it", and nothing checked.
type CallerResolver interface {
	// Caller returns the authenticated subject, or an error when there is none. Absence is an
	// error rather than an anonymous subject: a request with no identity must fail closed, and
	// a caller called "anonymous" is one that ends up on the chain as if it were somebody.
	Caller(request *http.Request) (mantlekeep.Subject, error)
}

// UserHeader is the header a fronting gateway sets to name the authenticated human or agent.
//
// It matches the core's own caller header, so one gateway configuration serves the door and
// every service in front of it. Brand-neutral deliberately: a header name is part of the wire
// contract a deployment operates, and a product name baked into it makes rebranding a
// breaking change rather than a configuration act.
const UserHeader = "X-Caller"

// GroupsHeader is the header a fronting gateway sets to name the caller's directory groups,
// comma-separated.
//
// It exists because roles are resolved from GROUPS on the SSO tier: the door is configured with a
// group->role table and refuses an identity whose groups map to nothing. A service that drops the
// groups therefore makes every caller unresolvable — and the refusal lands at IDENTITY, before any
// policy decision, so the door records nothing and an operator goes looking for a policy bug that
// does not exist.
//
// Groups are not roles, and that distinction is why forwarding them is safe: a caller may say
// which groups it is in, and never what those groups are worth. The door remains the only thing
// that decides what a group means.
//
// Brand-neutral and matching the door's own default, so one gateway configuration serves the door
// and every service in front of it.
const GroupsHeader = "X-Caller-Groups"

// HeaderCallers reads the caller from [UserHeader].
//
// It trusts the header, and that trust is only as good as what sits in front of it. This is the
// dev and gateway-fronted tier: something upstream — an IAP, an oauth2-proxy, a service
// mesh — has authenticated the person and stripped any client-supplied copy of the header. Run
// this with the port exposed and anyone can be anyone, which is why it is a named, chosen type
// rather than the behaviour you get by default.
type HeaderCallers struct{}

var _ CallerResolver = HeaderCallers{}

// Caller returns the subject named by the header.
func (HeaderCallers) Caller(request *http.Request) (mantlekeep.Subject, error) {
	name := strings.TrimSpace(request.Header.Get(UserHeader))
	if name == "" {
		return mantlekeep.Subject{}, fmt.Errorf(
			"no identity on the request — %s names the authenticated caller, and a change with "+
				"nobody behind it cannot be governed or attributed", UserHeader)
	}
	// The id and the gateway's asserted GROUPS travel; roles never do. Roles are the door's to
	// resolve from the directory, and a caller that could assert its own roles could assert its
	// way past any gate. A group is the input to that resolution, not a substitute for it.
	return mantlekeep.Subject{ID: name, ADGroups: assertedGroups(request)}, nil
}

// assertedGroups reads the groups the fronting gateway asserted.
//
// Blank entries are dropped: an empty group name matches nothing in a group->role table, and
// carrying one would put an empty string into the subject's groups, where it reads as a group
// that exists and is unmapped.
func assertedGroups(request *http.Request) []string {
	raw := request.Header.Get(GroupsHeader)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var groups []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			groups = append(groups, name)
		}
	}
	return groups
}
