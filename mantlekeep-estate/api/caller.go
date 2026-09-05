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
	// Only the ID travels. Roles are the door's to resolve from the directory; a caller that
	// could assert its own roles could assert its way past any gate.
	return mantlekeep.Subject{ID: name}, nil
}
