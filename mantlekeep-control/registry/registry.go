// Package registry is the generic governed artifact registry: any versioned
// template record — a build TOOL, a quant CALC model, a BEHAVIOUR — is REGISTERED
// as a draft, PROMOTED behind an approval gate, then DEPRECATED and DEMISED. Every
// transition is governed at the one door and pinned in the Store. One primitive,
// many faces (tool / calc / behaviour / container / service / memory); SDLC is a
// consumer, not a special case.
//
// Authoring is Superset-style (anyone drafts freely in a loose env); promotion is
// Confluence-style (draft → review → a required approver publishes). A silent
// version swap is a host audit finding, so resolution surfaces every substitution
// for the caller to record on the audit chain.
//
// Regulated organizations run a SEPARATE registry per env (dev/sit/uat/prod), each with its own
// PromotionPolicy — loose for dev/sit, sealed for uat/prod — behind one portal.
// Promotion between envs is a governed cross-instance handoff (see Promoter).
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	mantlekeep "mantlekeep.dev/control"
)

const (
	entryPrefix = "registry/"     // registry/<name> → Entry
	pinPrefix   = "registry-pin/" // registry-pin/<name>/<version>/<consumer> → blast-radius edge
)

// Kind tags the face an entry serves. Generic — the registry never learns SDLC.
type Kind string

// Status is the governed lifecycle of ONE version. Mirrors the product-template
// vocabulary (products.Manifest) and adds "demised" (retired, no longer resolves).
type Status string

const (
	StatusDraft      Status = "draft"      // authored, not yet proposed
	StatusInReview   Status = "review"     // promote proposed, awaiting a required approver
	StatusPublished  Status = "published"  // live in this env; pinned + immutable
	StatusDeprecated Status = "deprecated" // still resolves (old flows run) but warns
	StatusDemised    Status = "demised"    // retired; no longer resolves
)

// Version is one immutable release of an entry within a single env's registry.
type Version struct {
	Version     string          `json:"version"` // semver, e.g. "1.2.0"
	Env         string          `json:"env"`     // the env this record lives in
	Status      Status          `json:"status"`
	Ref         string            `json:"ref,omitempty"`        // artifact content digest, e.g. "sha256:…"
	Manifest    json.RawMessage   `json:"manifest,omitempty"`   // the template record
	Provenance  map[string]string `json:"provenance,omitempty"` // where the artifact came from (upload / git repo+commit)
	Default     bool              `json:"default,omitempty"`    // the env's fallback target
	ProposedBy  string          `json:"proposedBy,omitempty"`
	ApprovedBy  string          `json:"approvedBy,omitempty"`  // set on publish; SoD: != ProposedBy
	ChangeRef   string          `json:"changeRef,omitempty"`   // external CR id when the org routes prod via a change request
	TestPassed  bool            `json:"testPassed,omitempty"`  // a test-run has passed on this draft
	TestedAt    time.Time       `json:"testedAt,omitempty"`
	TestRef     string          `json:"testRef,omitempty"` // link to the test-run evidence (kernel I/O for a tool, a dev flow run)
	PublishedAt time.Time       `json:"publishedAt,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// Entry is a named artifact with many versions.
type Entry struct {
	Name     string    `json:"name"`
	Kind     Kind      `json:"kind"`
	Title    string    `json:"title,omitempty"`
	Owner    string    `json:"owner,omitempty"`
	Versions []Version `json:"versions"`
}

// Registry persists artifacts for ONE env in the shared Store (bolt today → any
// driver). Its PromotionPolicy is this env's restrictiveness — supplied per env.
type Registry struct {
	store  mantlekeep.Store
	env    string
	policy PromotionPolicy
	now    func() time.Time
}

// New builds a registry for env with the given promotion policy.
func New(store mantlekeep.Store, env string, policy PromotionPolicy) *Registry {
	return &Registry{store: store, env: env, policy: policy, now: func() time.Time { return time.Now().UTC() }}
}

// Env returns the environment this instance serves (dev/sit/uat/prod).
func (r *Registry) Env() string { return r.env }

// Register records a new DRAFT version. An unknown name creates the entry. A
// duplicate (name, version) is rejected — a version is immutable once it exists.
func (r *Registry) Register(ctx context.Context, name string, kind Kind, title, owner, version, ref string, manifest json.RawMessage) (Entry, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" {
		return Entry{}, fmt.Errorf("registry: name and version are required")
	}
	e, ok, err := r.get(ctx, name)
	if err != nil {
		return Entry{}, err
	}
	if !ok {
		e = Entry{Name: name, Kind: kind, Title: title, Owner: owner}
	}
	if indexOf(e, version) >= 0 {
		return Entry{}, fmt.Errorf("registry: %s@%s already exists (versions are immutable)", name, version)
	}
	e.Versions = append(e.Versions, Version{
		Version: version, Env: r.env, Status: StatusDraft,
		Ref: ref, Manifest: manifest, UpdatedAt: r.now(),
	})
	if err := r.save(ctx, e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Get returns one artifact and all its versions.
func (r *Registry) Get(ctx context.Context, name string) (Entry, bool, error) { return r.get(ctx, name) }

// List returns every artifact, sorted by name, with manifests omitted (metadata view).
func (r *Registry) List(ctx context.Context) ([]Entry, error) {
	keys, err := r.store.List(ctx, entryPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		raw, err := r.store.Get(ctx, k)
		if err != nil || raw == nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			continue
		}
		for i := range e.Versions {
			e.Versions[i].Manifest = nil // list view: metadata only
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *Registry) get(ctx context.Context, name string) (Entry, bool, error) {
	raw, err := r.store.Get(ctx, entryPrefix+safeName(name))
	if err != nil || raw == nil {
		// A missing key is "not present", not an error — some Store drivers (bolt)
		// return an error for an absent key, others nil; treat both the same, as catalog does.
		return Entry{}, false, nil
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Entry{}, false, err
	}
	return e, true, nil
}

func (r *Registry) save(ctx context.Context, e Entry) error {
	buf, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return r.store.Put(ctx, entryPrefix+safeName(e.Name), buf)
}

// indexOf returns the slice index of version in e, or -1.
func indexOf(e Entry, version string) int {
	for i := range e.Versions {
		if e.Versions[i].Version == version {
			return i
		}
	}
	return -1
}

// safeName stops a name traversing the keyspace with a slash.
func safeName(name string) string { return strings.ReplaceAll(name, "/", "_") }
