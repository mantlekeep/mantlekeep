package policybundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// stubSource is a policy source a test controls.
type stubSource struct {
	held   *grants.Grants
	floors *grants.Floors
	err    error
}

func (s stubSource) Load(context.Context) (*grants.Grants, *grants.Floors, grants.Revision, error) {
	if s.err != nil {
		return nil, nil, "", s.err
	}
	return s.held, s.floors, grants.RevisionOfDocuments(s.held, s.floors), nil
}

func policy(action string) stubSource {
	return stubSource{
		held: &grants.Grants{
			RoleActions:     map[string][]string{"L2-Operator": {action}},
			ApprovalActions: []string{"mr.approve"},
		},
		floors: &grants.Floors{Floors: map[string][]grants.FloorRule{
			action: {{Kind: "allowlist", Param: "app", Values: []string{"payments"}}},
		}},
	}
}

// unpack reads the files back out of a rendered bundle.
func unpack(t *testing.T, bundle Bundle) map[string][]byte {
	t.Helper()
	zipped, err := gzip.NewReader(bytes.NewReader(bundle.Body))
	if err != nil {
		t.Fatalf("the bundle is not gzip: %v", err)
	}
	files := map[string][]byte{}
	archive := tar.NewReader(zipped)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("the bundle is not a tar: %v", err)
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = content
	}
	return files
}

// THE POINT: a gateway evaluating this bundle and the door evaluating its own documents are
// reading the same policy, in the shape the engine's own Rego already addresses.
func TestTheBundleCarriesThePolicyInTheShapeTheRegoReads(t *testing.T) {
	bundle, err := Build(context.Background(), policy("deploy.dev"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	files := unpack(t, bundle)

	var data struct {
		Grants struct {
			RoleActions     map[string]any `json:"role_actions"`
			ApprovalActions []any          `json:"approval_actions"`
		} `json:"grants"`
		Floors map[string]any `json:"floors"`
	}
	if err := json.Unmarshal(files["data.json"], &data); err != nil {
		t.Fatalf("data.json is not readable: %v", err)
	}
	switch {
	case data.Grants.RoleActions["L2-Operator"] == nil:
		t.Fatal("data.grants.role_actions is missing the role — the Rego reads this path")
	case len(data.Grants.ApprovalActions) != 1:
		t.Fatal("data.grants.approval_actions is empty — the AI guardrail reads this path, and " +
			"an empty list means the gateway thinks nothing is approval-shaped")
	case data.Floors["deploy.dev"] == nil:
		t.Fatal("data.floors is missing the action's rules — the floor reads this path")
	}
}

// The revision is what makes "the gateway and the door agree" checkable rather than asserted.
func TestTheBundleIsTaggedWithTheRevisionOfThePolicyInsideIt(t *testing.T) {
	source := policy("deploy.dev")
	_, _, expected, _ := source.Load(context.Background())

	bundle, err := Build(context.Background(), source)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if bundle.Revision != expected {
		t.Fatalf("the bundle reports revision %s for policy whose revision is %s — an operator "+
			"comparing the gateway to the door would conclude they differ when they do not",
			bundle.Revision, expected)
	}
	var manifest struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(unpack(t, bundle)[".manifest"], &manifest); err != nil {
		t.Fatalf(".manifest is not readable: %v", err)
	}
	if manifest.Revision != string(expected) {
		t.Fatalf("the manifest says %q; OPA reports that on its status API, so a running "+
			"gateway would name the wrong policy", manifest.Revision)
	}
}

// Two replicas rendering the same policy must produce the same BYTES. Otherwise every checksum
// and cache downstream reports a change that did not happen.
func TestTheSamePolicyRendersToTheSameBytes(t *testing.T) {
	first, err := Build(context.Background(), policy("deploy.dev"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(context.Background(), policy("deploy.dev"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Body, second.Body) {
		t.Fatal("the same policy rendered to different bytes — every cache downstream would see " +
			"a change on every poll")
	}
	// Asserted directly rather than inferred from two builds matching: a clock-derived
	// timestamp is written to the tar with SECOND granularity, so two builds in the same
	// second would agree by luck and the test would pass on a bundle that is not reproducible.
	zipped, err := gzip.NewReader(bytes.NewReader(first.Body))
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(zipped)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !header.ModTime.Equal(time.Unix(0, 0).UTC()) {
			t.Fatalf("%s carries modification time %s — the bundle is stamped with WHEN it was "+
				"built, so identical policy produces different bytes on every render",
				header.Name, header.ModTime)
		}
	}
	changed, err := Build(context.Background(), policy("deploy.prod"))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision == first.Revision {
		t.Fatal("a DIFFERENT policy produced the same revision — nothing downstream would reload")
	}
}

// A failed read must be an error. Empty grants deny everything, so a gateway handed an empty
// bundle enforces a working deny-all while every log says the update succeeded.
func TestAFailedReadIsAnErrorNeverAnEmptyBundle(t *testing.T) {
	_, err := Build(context.Background(), stubSource{err: errors.New("the policy store is down")})
	if err == nil {
		t.Fatal("a failed source produced a bundle")
	}
	if _, err := Build(context.Background(), nil); err == nil {
		t.Fatal("a nil source produced a bundle")
	}
	if _, err := Build(context.Background(), stubSource{}); err == nil {
		t.Fatal("a source that returned no documents and no error produced a bundle")
	}
}
