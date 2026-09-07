// Package policybundle serves the policy documents in force as an OPA bundle.
//
// It exists so a gateway and the door can enforce ONE policy rather than two copies of it.
//
// A gateway answers a question the door cannot: may this request reach this route, per request,
// in microseconds, for traffic that never becomes a governed intent. That decision has to be made
// locally — a synchronous call to a shared decision service fails open (catastrophe) or closed
// (outage) on every request, and it cannot run behind an airlock at all. So the gateway keeps its
// own evaluator, and what gets centralised is the DOCUMENTS it evaluates.
//
// # What it serves
//
// The same shape the engine's own OPA adapter builds, so the same Rego reads either:
//
//	data.grants.role_actions
//	data.grants.approval_actions
//	data.floors[action]
//
// # Why the revision is the point
//
// The bundle is tagged with the revision [grants.RevisionOf] derives from the documents. It is
// the SAME string the door reports for the same policy, so "the gateway and the door are enforcing
// the same rules" stops being an assurance and becomes a comparison anyone can run. Two components
// reporting different revisions are demonstrably serving different policy — a fact an operator can
// act on rather than a divergence nobody notices.
//
// A revision derived from a row id, a timestamp or a build number could not do this: it would
// describe when the policy was stored, not what it says.
//
// # What it does not do
//
// It links no OPA. A bundle is a gzipped tar of JSON, which the standard library makes; depending
// on the engine in order to feed the engine would put a policy runtime inside the core for no
// reason. It also decides nothing, holds no documents of its own, and reads through whatever
// [grants.Loader] it was given — files, Postgres, or anything a deployment writes.
package policybundle
