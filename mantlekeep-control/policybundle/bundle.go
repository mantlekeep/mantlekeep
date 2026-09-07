package policybundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// Bundle is a rendered OPA bundle and the revision that identifies the policy inside it.
type Bundle struct {
	// Body is the gzipped tar an OPA instance downloads.
	Body []byte
	// Revision identifies the DOCUMENTS, not the file: rendering the same policy twice
	// produces the same revision, so a gateway and the door can be compared.
	Revision grants.Revision
}

// Build renders the policy in force as an OPA bundle.
//
// An error is an error, never an empty bundle. Empty grants deny everything, so a source that
// failed and one that legitimately grants nothing would be indistinguishable — and a gateway
// handed the first would enforce a working deny-all while reporting success.
func Build(ctx context.Context, source grants.Loader) (Bundle, error) {
	if source == nil {
		return Bundle{}, fmt.Errorf("policybundle: a bundle needs a policy source")
	}
	held, floors, revision, err := source.Load(ctx)
	if err != nil {
		return Bundle{}, fmt.Errorf("policybundle: reading the policy in force: %w", err)
	}
	if held == nil || floors == nil {
		return Bundle{}, fmt.Errorf("policybundle: the source returned no documents and no error")
	}

	// The shape the engine's own adapter builds, so one Rego serves both.
	data, err := json.Marshal(map[string]any{
		"grants": map[string]any{
			"role_actions":     held.RoleActionsAny(),
			"approval_actions": held.ApprovalActionsAny(),
		},
		"floors": floors.FloorsAny(),
	})
	if err != nil {
		return Bundle{}, fmt.Errorf("policybundle: rendering the documents: %w", err)
	}
	// OPA reads .manifest for the revision it reports back on its status API — which is how an
	// operator asks a running gateway what it is enforcing without trusting a deployment log.
	manifest, err := json.Marshal(map[string]any{"revision": string(revision), "roots": []string{""}})
	if err != nil {
		return Bundle{}, fmt.Errorf("policybundle: rendering the manifest: %w", err)
	}

	body, err := targz(map[string][]byte{".manifest": manifest, "data.json": data})
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Body: body, Revision: revision}, nil
}

// targz writes the named files as a gzipped tar.
//
// Written with a FIXED modification time and in a fixed order, so the same policy renders to the
// same bytes. Otherwise two replicas serving identical policy would hand out bundles that differ,
// and every cache and checksum downstream would report a change that did not happen.
func targz(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(zipped)

	for _, name := range []string{".manifest", "data.json"} {
		content, ok := files[name]
		if !ok {
			continue
		}
		header := &tar.Header{
			Name:    name,
			Mode:    0o600,
			Size:    int64(len(content)),
			ModTime: time.Unix(0, 0).UTC(),
		}
		if err := archive.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("policybundle: writing %s: %w", name, err)
		}
		if _, err := archive.Write(content); err != nil {
			return nil, fmt.Errorf("policybundle: writing %s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("policybundle: closing the archive: %w", err)
	}
	if err := zipped.Close(); err != nil {
		return nil, fmt.Errorf("policybundle: closing the gzip stream: %w", err)
	}
	return buffer.Bytes(), nil
}
