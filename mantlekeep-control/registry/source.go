package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Artifact is content + provenance a Source produced, ready to Register. Digest is
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

// Source ingests artifact content from SOMEWHERE — an upload, a git repo, an OCI
// registry, a local path — and returns it content-addressed with provenance. HOW the
// bytes arrive is the adapter's job; the registry only stores the resulting digest.
// Add a new source by writing an adapter; nothing about ingestion is hardcoded.
type Source interface {
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

var _ Source = UploadSource{}

// GitFetcher clones repo@ref and returns the packaged artifact bytes, the resolved
// commit, and any template descriptor found in the repo (e.g. a mantlekeep.tool.yaml) so
// the draft template can be generated from the source. Its impl (shell to git + a
// build worker) lives OUTSIDE the lean core so the core never links git — it is
// injected into GitSource, ports-and-adapters style. This is the local/SIT ingress;
// above SIT the built artifact is promoted, not re-fetched (see Promoter).
type GitFetcher func(ctx context.Context, repo, ref string) (bytes []byte, commit string, descriptor []byte, err error)

// GitSource ingests from git: fetch source at a ref (build via the injected fetcher),
// content-address the result, and record repo+commit provenance for the audit chain.
type GitSource struct{ Fetcher GitFetcher }

// Fetch runs the injected fetcher and content-addresses its output.
func (g GitSource) Fetch(ctx context.Context, req SourceRequest) (Artifact, error) {
	if g.Fetcher == nil {
		return Artifact{}, errors.New("git source: no fetcher injected")
	}
	if req.Repo == "" {
		return Artifact{}, errors.New("git source: repo is required")
	}
	b, commit, descriptor, err := g.Fetcher(ctx, req.Repo, req.Ref)
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

var _ Source = GitSource{}

// Ingest fetches an artifact from a Source and registers it as a DRAFT, stamping the
// content digest (Ref) and provenance. This is how an upload OR a git fetch becomes a
// governed draft — identical downstream lifecycle regardless of where the bytes came
// from.
func (r *Registry) Ingest(ctx context.Context, src Source, req SourceRequest, name string, kind Kind, title, owner, version string, manifest json.RawMessage) (Version, error) {
	art, err := src.Fetch(ctx, req)
	if err != nil {
		return Version{}, err
	}
	// Generate the template from the source's descriptor when the caller supplies none.
	tmpl := manifest
	if len(tmpl) == 0 {
		tmpl = art.Manifest
	}
	if _, err := r.Register(ctx, name, kind, title, owner, version, art.Digest, tmpl); err != nil {
		return Version{}, err
	}
	return r.transition(ctx, name, version, func(v *Version) error {
		v.Provenance = art.Provenance
		return nil
	})
}

// digest content-addresses bytes as "sha256:<hex>".
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
