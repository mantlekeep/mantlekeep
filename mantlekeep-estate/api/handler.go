// Package api is the estate's HTTP transport — a thin adapter, not a UI.
//
// It parses, resolves WHO is calling, delegates, and encodes the answer. It decides nothing.
// Every governance question belongs to the [estate.Manager] behind it, which submits each change
// to the door before an adapter touches anything; every question about what exists belongs to
// the [estate.Service], which never calls the door at all.
//
// That read/write split is the one structural rule here. The door records DECISIONS, and
// routing queries through it buries them in traffic — an auditor reading the chain would have
// to find a handful of changes among thousands of page loads. So writes go through the manager
// and reads go straight to the service.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// maxManifest bounds an accepted manifest. A footprint declaration is a small document; the
// limit exists so an unbounded body cannot be used to exhaust the process before anything has
// even been parsed.
const maxManifest = 1 << 20

// Handler serves the estate API.
type Handler struct {
	// manager governs and applies. Writes go here, and it is the only path to the door.
	manager *estate.Manager
	// service answers reads. It never calls the door — see the package comment.
	service *estate.Service
	// callers says who is on the other end of the call, resolved from the transport.
	callers Callers
}

// New wires the controller to its collaborators. One constructor, everything explicit, so a
// test hands in exactly the manager, service and identity source it means to exercise.
func New(manager *estate.Manager, service *estate.Service, callers Callers) *Handler {
	return &Handler{manager: manager, service: service, callers: callers}
}

// Routes registers the estate API on a mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/estate/{team}", h.apply)
	mux.HandleFunc("GET /api/estate/{team}", h.read)
	mux.HandleFunc("POST /api/estate/{team}/reconcile", h.reconcile)
	// Approvals. A gated change is the normal outcome at prod, so a person needs somewhere to
	// stand — these are that. Listing is per team, or across all of them for whoever is the
	// gate: an approver who cannot see their queue is not a gate, they are a bottleneck.
	mux.HandleFunc("GET /api/approvals", h.pendingApprovals)
	mux.HandleFunc("GET /api/estate/{team}/approvals", h.pendingApprovals)
	mux.HandleFunc("POST /api/approvals/{id}/approve", h.approve)
	mux.HandleFunc("POST /api/approvals/{id}/decline", h.decline)
}

// apply governs and applies a team's declared footprint.
//
// The manifest is the body; the ACTOR is not, and never can be. Whoever is named by the
// transport is who the door rules on and who the chain records — a manifest that could name its
// own author would let any caller act as anyone by editing a field.
func (h *Handler) apply(writer http.ResponseWriter, request *http.Request) {
	actor, err := h.callers.Caller(request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}

	document, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxManifest))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "body: "+err.Error())
		return
	}
	manifest, err := estate.ParseManifest(document)
	if err != nil {
		// The parser's own message, which already names the offending field — and, for an
		// unknown field, says so rather than dropping it. Rephrasing it here would lose the
		// name and leave the author guessing which line to fix.
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if team := request.PathValue("team"); manifest.Team != team {
		writeError(writer, http.StatusBadRequest, "team: the manifest declares team "+
			quote(manifest.Team)+" but was posted to "+quote(team)+" — a footprint applied under "+
			"another team's name would be governed under that team's scope")
		return
	}

	outcome, err := h.manager.Apply(request.Context(), actor, manifest)
	if err != nil {
		writeError(writer, applyFailureStatus(err), err.Error())
		return
	}
	writeJSON(writer, statusFor(outcome), outcome)
}

// read returns the team's estate: what was declared, what it resolves to, what exists, and where
// they disagree.
//
// Straight to the service. No intent is submitted, because nothing happens as a result of a
// read and there is no decision to record.
func (h *Handler) read(writer http.ResponseWriter, request *http.Request) {
	// Authenticated, though not governed. A read of another team's estate is still a
	// disclosure, and an endpoint that answers anyone is one nobody can put on a network.
	if _, err := h.callers.Caller(request); err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}

	footprint, err := h.service.Footprint(request.Context(), request.PathValue("team"))
	switch {
	case errors.Is(err, estate.ErrUnknownTeam):
		writeError(writer, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(writer, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(writer, http.StatusOK, footprint)
	}
}

// reconcileReport is one pass: what it closed, and what it refused to close on its own.
//
// Escalated is a separate field rather than a bucket inside the outcome because it is a
// different kind of fact. The outcome says what the reconciler DID; escalated says what it
// deliberately did not do because a person has to — and that list is the one somebody must
// actually read.
type reconcileReport struct {
	Outcome   estate.ApplyOutcome `json:"outcome"`
	Escalated []estate.Drift      `json:"escalated"`
}

// reconcile runs one pass over the team's footprint.
func (h *Handler) reconcile(writer http.ResponseWriter, request *http.Request) {
	actor, err := h.callers.Caller(request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}

	// The declared footprint comes from the store, not the body. A reconcile that accepted a
	// manifest would let a caller choose what reality is compared against, which is a way to
	// make drift disappear by redefining it.
	manifest, err := h.service.Manifest(request.Context(), request.PathValue("team"))
	switch {
	case errors.Is(err, estate.ErrUnknownTeam):
		writeError(writer, http.StatusNotFound, err.Error())
		return
	case err != nil:
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}

	outcome, escalated, err := h.manager.Reconcile(request.Context(), actor, manifest)
	if err != nil {
		writeError(writer, http.StatusBadGateway, err.Error())
		return
	}
	if escalated == nil {
		escalated = []estate.Drift{}
	}
	writeJSON(writer, statusFor(outcome), reconcileReport{Outcome: outcome, Escalated: escalated})
}

// applyFailureStatus separates our fault from the caller's.
//
// A store that will not write is an outage and must not be dressed up as a bad request: the
// team would go and re-read a manifest that was never the problem. Everything else Apply can
// fail on is the manifest meeting the floor — a runtime this deployment does not serve, a tier
// it has no limits for — which the author can act on.
func applyFailureStatus(err error) int {
	if errors.Is(err, estate.ErrStoreFailure) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func quote(value string) string { return "\"" + value + "\"" }

// pendingApprovals lists changes waiting for a person.
func (h *Handler) pendingApprovals(writer http.ResponseWriter, request *http.Request) {
	if _, err := h.callers.Caller(request); err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}
	waiting, err := h.manager.PendingApprovals(request.Context(), request.PathValue("team"))
	if err != nil {
		writeError(writer, http.StatusBadGateway, err.Error())
		return
	}
	// Empty rather than null. A reader that has to treat null and [] as the same thing will one
	// day treat one of them as "unknown" instead.
	if waiting == nil {
		waiting = []estate.Approval{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"pending": waiting})
}

// approve applies a change a person has signed off.
//
// No role travels in this request, deliberately. The estate submits AS the approver and the door
// resolves their roles from the directory — a caller that could assert its own roles could
// assert its way past any gate.
func (h *Handler) approve(writer http.ResponseWriter, request *http.Request) {
	caller, err := h.callers.Caller(request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}
	result, approveErr := h.manager.Approve(request.Context(), caller, request.PathValue("id"))
	if approveErr != nil {
		writeError(writer, statusForApproval(approveErr), approveErr.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"result": result})
}

// decline records that a person refused, with their reason.
func (h *Handler) decline(writer http.ResponseWriter, request *http.Request) {
	caller, err := h.callers.Caller(request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil &&
		!errors.Is(decodeErr, io.EOF) {
		writeError(writer, http.StatusBadRequest, "decline: "+decodeErr.Error())
		return
	}
	if declineErr := h.manager.Decline(request.Context(), caller, request.PathValue("id"),
		body.Reason); declineErr != nil {
		writeError(writer, statusForApproval(declineErr), declineErr.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"declined": request.PathValue("id")})
}

// statusForApproval maps an approval error onto a status a caller can act on.
//
// Each of these sends a person somewhere different, which is the whole reason they are not one
// code: nothing to act on, already decided, or a rule that will not bend.
func statusForApproval(err error) int {
	switch {
	case errors.Is(err, estate.ErrApprovalNotFound):
		return http.StatusNotFound
	case errors.Is(err, estate.ErrApprovalNotPending):
		return http.StatusConflict
	case errors.Is(err, estate.ErrSelfApproval), errors.Is(err, estate.ErrFloorMoved):
		return http.StatusForbidden
	default:
		return http.StatusBadGateway
	}
}
