package policybundle

import (
	"log/slog"
	"net/http"
	"strings"
)

// Handler serves the policy in force as an OPA bundle over HTTP.
//
// Point an OPA instance at it with the usual bundle configuration:
//
//	services:  [{name: mantlekeep, url: http://control-plane:8080}]
//	bundles:   {policy: {service: mantlekeep, resource: /policy/bundle.tar.gz}}
//
// OPA then polls, caches, and swaps atomically on a real change — the same
// validate-then-swap discipline the door applies to its own documents.
type Handler struct {
	build func() (Bundle, error)
	log   *slog.Logger
}

// NewHandler serves bundles built from source on every request.
//
// It rebuilds per request rather than caching, because the thing it is serving is the policy IN
// FORCE and a cache is a second copy that can be stale. The revision makes that cheap in practice:
// a caller that already holds the current revision is answered 304 with no body.
func NewHandler(build func() (Bundle, error), logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{build: build, log: logger}
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "bundle: read only", http.StatusMethodNotAllowed)
		return
	}
	bundle, err := h.build()
	if err != nil {
		// 503, not 500, and not an empty bundle. An OPA that receives an error keeps the
		// policy it already has; one that receives an empty bundle installs a deny-all and
		// takes the gateway down with it. Failing the request is the safe answer.
		h.log.Error("policy bundle unavailable — callers keep their last-good policy", "error", err)
		http.Error(writer, "policy bundle unavailable", http.StatusServiceUnavailable)
		return
	}

	tag := `"` + string(bundle.Revision) + `"`
	writer.Header().Set("ETag", tag)
	// The revision by name as well as by ETag: an operator diagnosing a split-brain wants to
	// curl this and compare it to what the door reports, without decoding cache semantics.
	writer.Header().Set("X-Policy-Revision", string(bundle.Revision))
	writer.Header().Set("Content-Type", "application/gzip")
	writer.Header().Set("Cache-Control", "no-cache")

	// A poll that finds nothing new costs no body. Most polls find nothing new.
	if matchesETag(request.Header.Get("If-None-Match"), tag) {
		writer.WriteHeader(http.StatusNotModified)
		return
	}
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(bundle.Body)
}

// matchesETag reports whether the caller already holds this revision.
//
// If-None-Match may carry a list, and a proxy is allowed to weaken a tag by prefixing W/. Both
// are handled here so an intermediary cannot turn "nothing changed" into a full re-download of
// every policy on every poll.
func matchesETag(header, tag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == tag {
			return true
		}
	}
	return false
}
