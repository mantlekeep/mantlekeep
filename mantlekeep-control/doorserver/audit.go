package doorserver

import (
	"context"
	"net/http"

	mantlekeep "mantlekeep.dev/control"
)

// recordLister is the optional ability to read the chain back. AuditLogger itself only
// appends and verifies — reading is not required to govern — so the server asks for it
// by interface and degrades to verification-only if the logger cannot list.
type recordLister interface {
	Records(ctx context.Context, limit int) ([]mantlekeep.AuditRecord, error)
}

// auditRecordLimit caps a single audit view. The chain is append-only and unbounded;
// an unpaged "give me everything" would eventually be a denial of service against the
// door it is meant to prove.
const auditRecordLimit = 500

// handleAudit serves the chain view: whether it verifies, and the recent records.
//
// `intact` is the load-bearing field — it re-walks the hash links and reports whether
// the ledger has been tampered with. Clients call it to assert the guarantee, so it is
// computed on every request rather than cached.
func (s *Server) handleAudit(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.resolveCaller(request); err != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"error": err.Error()})
		return
	}

	intact, err := s.door.Audit.Verify(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError,
			map[string]any{"error": "cannot verify the chain: " + err.Error()})
		return
	}

	records := []map[string]any{}
	if lister, canList := s.door.Audit.(recordLister); canList {
		stored, listErr := lister.Records(request.Context(), auditRecordLimit)
		if listErr != nil {
			writeJSON(writer, http.StatusInternalServerError,
				map[string]any{"error": "cannot read the chain: " + listErr.Error()})
			return
		}
		for _, record := range stored {
			records = append(records, wireRecord(record))
		}
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"intact":  intact,
		"count":   len(records),
		"records": records,
	})
}

// wireRecord projects a stored record onto the wire. It is a deliberate SUBSET: the
// hashes and the decision are what prove the chain, while internal identifiers stay
// server-side. `subject`, `action` and `decision` are the frozen names SDK clients read.
func wireRecord(record mantlekeep.AuditRecord) map[string]any {
	return map[string]any{
		"timestamp": record.Timestamp,
		"subject":   record.SubjectID,
		"action":    record.Action,
		"decision":  string(record.Decision),
		"policy":    record.PolicyID,
		"is_ai":     record.IsAI,
		"via":       record.Via,
		"hash":      record.Hash,
		"prev_hash": record.PrevHash,
	}
}
