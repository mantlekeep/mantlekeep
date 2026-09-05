package grants

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Loader is WHERE the policy documents come from.
//
// Named for its method, per Go's convention for single-method interfaces, and so it pairs
// with [Writer]. The sibling seams in internal/policy and registry are called Source; they
// predate this and are the same idea under an older name.
//
// The core defines the question and never learns the answer's technology. A file today, a
// database table for a deployment that edits policy through a UI, a signed bundle server for
// one that treats policy as a release artefact — each is an adapter, and each carries its own
// dependency rather than putting a driver in this module.
//
// It is the same seam as the layered-config Loader in internal/policy and as the estate's
// Port: the thing above states what it needs, the thing below decides how.
//
// # Why a Revision travels with the documents
//
// A deployment that can change policy has, by definition, more than one policy over time —
// and a decision recorded without saying WHICH policy made it cannot be reviewed later. The
// revision is derived from the CONTENT rather than declared, because a version somebody types
// can be stale or wrong while a hash of what was actually loaded cannot.
//
// It is also what makes a single source of truth verifiable rather than merely intended: two
// replicas reporting different revisions are demonstrably serving different policy, which is
// a fact an operator can act on instead of a divergence nobody notices.
type Loader interface {
	// Load returns the documents in force and the revision that identifies them.
	//
	// An error is an error, never an empty policy. Empty grants deny everything, so a source
	// that failed and one that legitimately grants nothing would be indistinguishable — and
	// the first would look like a working deny-all rather than an outage.
	Load(ctx context.Context) (*Grants, *Floors, Revision, error)
}

// Revision identifies a policy document set by its content.
type Revision string

// RevisionOf derives a revision from the documents as loaded.
//
// Content-addressed on purpose: the same policy always produces the same revision, wherever
// it came from and however it was stored, so a file deployment and a database deployment
// serving identical policy agree — which is what lets one be migrated to the other and proved
// equivalent rather than assumed to be.
func RevisionOf(documents ...[]byte) Revision {
	digest := sha256.New()
	for _, document := range documents {
		// Length-prefixed, so two documents cannot be concatenated into a third that hashes
		// the same. Without it, ["ab","c"] and ["a","bc"] are one revision.
		_, _ = fmt.Fprintf(digest, "%d:", len(document))
		_, _ = digest.Write(document)
	}
	return Revision(hex.EncodeToString(digest.Sum(nil))[:16])
}

// EnvSource is the source this binary uses when a deployment configures nothing else: the
// embedded defaults, overridden by the MANTLEKEEP_POLICY_* environment.
//
// It exists so the port has a working implementation from the first commit rather than an
// interface nobody satisfies, and so the existing behaviour is reachable THROUGH the seam
// rather than beside it.
type EnvSource struct{}

// Load implements [Loader] over the environment-configured documents.
func (EnvSource) Load(context.Context) (*Grants, *Floors, Revision, error) {
	grants, err := Load()
	if err != nil {
		return nil, nil, "", err
	}
	floors, err := LoadFloors()
	if err != nil {
		return nil, nil, "", err
	}
	// The revision is derived from the RESOLVED documents rather than the raw files, because
	// what governs is what was merged: two deployments with different files that resolve to
	// the same policy are serving the same policy, and should say so.
	return grants, floors, RevisionOf(canonical(grants), canonical(floors)), nil
}

// canonical renders a document for hashing.
//
// json.Marshal sorts map keys, so the same policy hashes the same however it was authored or
// stored — which is the property that makes two deployments comparable. A document that
// cannot be marshalled hashes as its error text rather than as nothing: an unhashable policy
// must not quietly share a revision with every other unhashable policy.
func canonical(document any) []byte {
	encoded, err := json.Marshal(document)
	if err != nil {
		return []byte("unmarshalable: " + err.Error())
	}
	return encoded
}
