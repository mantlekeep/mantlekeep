package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Artifact is content + provenance a Fetcher produced, ready to Register. Digest is
// the content address; Provenance records where it came from; Manifest is a template
// descriptor DISCOVERED in the source (e.g. a mantlekeep.tool.yaml in the repo) so the
// draft template can be GENERATED from the repo instead of hand-typed.
type Artifact struct {
	Bytes      []byte
	Digest     string            // "sha256:…"
	Manifest   json.RawMessage   // template descriptor found in the source, if any (generate-from-repo)
	Provenance map[string]string // e.g. {"source":"git","repo":…,"commit":…}
}

// SourceRequest names what to ingest; each adapter interprets the fields it needs.
type SourceRequest struct {
	Data []byte            // inline bytes (upload)
	Repo string            // repo url (git)
	Ref  string            // git ref / tag / commit
	Opts map[string]string // adapter-specific, e.g. {"by":"alice"}
}

// Fetcher ingests artifact content from SOMEWHERE — an upload, a git repo, an OCI
// registry, a local path — and returns it content-addressed with provenance. HOW the
// bytes arrive is the adapter's job; the registry only stores the resulting digest.
// Add a new source by writing an adapter; nothing about ingestion is hardcoded.
type Fetcher interface {
	Fetch(ctx context.Context, req SourceRequest) (Artifact, error)
}

// UploadSource is the pass-through adapter: the caller already holds the built bytes
// (a portal multipart upload). It just content-addresses them. Pure stdlib, so it
// lives in the lean core.
type UploadSource struct{}

// Fetch content-addresses the uploaded bytes.
func (UploadSource) Fetch(_ context.Context, req SourceRequest) (Artifact, error) {
	if len(req.Data) == 0 {
		return Artifact{}, errors.New("upload source: no data")
	}
	return Artifact{
		Bytes:      req.Data,
		Digest:     digest(req.Data),
		Provenance: map[string]string{"source": "upload", "by": req.Opts["by"]},
	}, nil
}

var _ Fetcher = UploadSource{}

// GitCloner clones repo@ref and returns the packaged artifact bytes, the resolved
// commit, and any template descriptor found in the repo (e.g. a mantlekeep.tool.yaml) so
// the draft template can be generated from the source. Its impl (shell to git + a
// build worker) lives OUTSIDE the lean core so the core never links git — it is
// injected into GitSource, ports-and-adapters style. This is the local/SIT ingress;
// above SIT the built artifact is promoted, not re-fetched (see Promoter).
//
// It is deliberately NOT the Fetcher interface above: Fetcher is the ingestion port the
// registry calls, this is the narrower git-clone port GitSource depends on.
type GitCloner func(ctx context.Context, repo, ref string) (bytes []byte, commit string, descriptor []byte, err error)

// GitSource ingests from git: fetch source at a ref (build via the injected cloner),
// content-address the result, and record repo+commit provenance for the audit chain.
type GitSource struct{ Clone GitCloner }

// Fetch runs the injected cloner and content-addresses its output.
func (g GitSource) Fetch(ctx context.Context, req SourceRequest) (Artifact, error) {
	if g.Clone == nil {
		return Artifact{}, errors.New("git source: no cloner injected")
	}
	if req.Repo == "" {
		return Artifact{}, errors.New("git source: repo is required")
	}
	b, commit, descriptor, err := g.Clone(ctx, req.Repo, req.Ref)
	if err != nil {
		return Artifact{}, fmt.Errorf("git source: %w", err)
	}
	return Artifact{
		Bytes:    b,
		Digest:   digest(b),
		Manifest: descriptor, // generate the template from the repo's descriptor, if present
		Provenance: map[string]string{
			"source": "git", "repo": req.Repo, "ref": req.Ref, "commit": commit,
		},
	}, nil
}

var _ Fetcher = GitSource{}

// Ingest fetches an artifact from a Fetcher and registers it as a DRAFT, stamping the
// content digest (Ref) and provenance. This is how an upload OR a git fetch becomes a
// governed draft — identical downstream lifecycle regardless of where the bytes came
// from.
//
// Two fields of reg are supplied by the FETCH, not the caller: Ref is always the digest
// of the bytes that arrived (anything set here is replaced — the registry content-
// addresses what it actually stored, never what it was told), and an empty Manifest
// falls back to the descriptor discovered in the source.
func (r *Registry) Ingest(ctx context.Context, src Fetcher, req SourceRequest, reg Registration) (Version, error) {
	art, err := src.Fetch(ctx, req)
	if err != nil {
		return Version{}, err
	}
	reg.Ref = art.Digest
	// Generate the template from the source's descriptor when the caller supplies none.
	if len(reg.Manifest) == 0 {
		reg.Manifest = art.Manifest
	}
	if _, err := r.Register(ctx, reg); err != nil {
		return Version{}, err
	}
	return r.transition(ctx, reg.Name, reg.Version, func(v *Version) error {
		v.Provenance = art.Provenance
		return nil
	})
}

// digest content-addresses bytes as "sha256:<hex>".
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
