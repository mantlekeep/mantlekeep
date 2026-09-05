package pgpolicy

import (
	"encoding/json"
	"fmt"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// documents is the decoded policy: the pair of documents that govern together.
//
// They travel as a pair because the revision covers both. A deployment that changed a floor
// and kept the same grants has changed its policy, and a revision that only hashed the
// grants would say it had not.
type documents struct {
	grants *grants.Grants
	floors *grants.Floors
}

// decode turns a stored snapshot into the documents in force.
//
// It normalises exactly as the core's file source does — a nil map becomes an empty one —
// and no more. That equality is the whole point: normalise differently here and a database
// deployment would report a different revision from a file deployment holding the identical
// policy, which is precisely the comparison the revision exists to make possible.
func decode(snapshot Snapshot) (documents, error) {
	var decoded documents

	decoded.grants = &grants.Grants{}
	if err := json.Unmarshal(snapshot.GrantsDoc, decoded.grants); err != nil {
		// Wrapped, never swallowed. A grant document that does not parse is an outage, and
		// answering it with an empty policy would make it look like a deliberate deny-all.
		return documents{}, fmt.Errorf("%w: grants: %v", ErrCorruptDocument, err)
	}
	if decoded.grants.RoleActions == nil {
		decoded.grants.RoleActions = map[string][]string{}
	}

	decoded.floors = &grants.Floors{}
	if err := json.Unmarshal(snapshot.FloorsDoc, decoded.floors); err != nil {
		return documents{}, fmt.Errorf("%w: floors: %v", ErrCorruptDocument, err)
	}
	if decoded.floors.Floors == nil {
		decoded.floors.Floors = map[string][]grants.FloorRule{}
	}

	return decoded, nil
}

// encode renders the documents back to the bytes the store holds, and derives the revision
// they carry.
//
// The revision is derived from the RESOLVED documents rather than from whatever bytes
// happened to be in the row, for the same reason the core derives it from the resolved
// document rather than from the raw file: what governs is what was merged. Two rows that
// differ only in whitespace or key order hold the same policy and must report the same
// revision.
func encode(decoded documents) (Snapshot, grants.Revision, error) {
	grantsDoc, err := json.Marshal(decoded.grants)
	if err != nil {
		// Unreachable for these types — they are maps, slices and strings, none of which
		// json.Marshal can refuse. Reported rather than papered over anyway, because the
		// alternative is a policy whose revision is a description of an error.
		return Snapshot{}, "", fmt.Errorf("pgpolicy: cannot render the grant document: %w", err)
	}
	floorsDoc, err := json.Marshal(decoded.floors)
	if err != nil {
		return Snapshot{}, "", fmt.Errorf("pgpolicy: cannot render the floor document: %w", err)
	}

	// grants.RevisionOf, not a hash of our own: the core owns what a revision means, and a
	// second implementation of it here would be a second answer that could drift from the
	// first without anything failing.
	revision := grants.RevisionOf(grantsDoc, floorsDoc)
	return Snapshot{GrantsDoc: grantsDoc, FloorsDoc: floorsDoc, StoredRevision: revision}, revision, nil
}

// Encode renders a policy in the exact form this store holds it, and returns the revision it
// will report once stored.
//
// This is the migration bridge, in both directions. Seeding Postgres from the file source
// means loading through [grants.Loader], calling Encode, and inserting what it returns; the
// revision it hands back is the one the database will serve, so an operator can compare it
// with the file deployment's and PROVE the two are equivalent instead of assuming it. Going
// the other way, the bytes it returns are a valid MANTLEKEEP_POLICY_GRANTS /
// MANTLEKEEP_POLICY_FLOORS document.
func Encode(policyGrants *grants.Grants, policyFloors *grants.Floors) (Snapshot, grants.Revision, error) {
	if policyGrants == nil || policyFloors == nil {
		// Refused, not defaulted. A nil document silently treated as an empty one is a
		// deny-all that nobody wrote down.
		return Snapshot{}, "", fmt.Errorf("pgpolicy: Encode needs both documents; a missing one is not an empty one")
	}
	// Round-tripped through decode so that what Encode produces and what Load reads back are
	// normalised by the same code, rather than by two functions that must be kept in step.
	rendered, _, err := encode(documents{grants: policyGrants, floors: policyFloors})
	if err != nil {
		return Snapshot{}, "", err
	}
	normalised, err := decode(rendered)
	if err != nil {
		return Snapshot{}, "", err
	}
	return encode(normalised)
}
