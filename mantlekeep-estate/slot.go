package estate

import (
	"fmt"
	"strings"
)

// Slot is WHERE something runs — the identity a deployment actually has.
//
// Not just a cluster. Two versions of one app commonly run side by side in the same cluster in
// different namespaces during a changeover, and keying on cluster alone collides them: the
// reconciler sees one as drift from the other and "fixes" it by overwriting a live version.
//
// Cluster plus namespace plus name is also Kubernetes' own identity, so nothing is invented
// here — the model is simply catching up with how the platform already thinks.
type Slot struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Key is the stable identity used to compare desired against observed.
func (s Slot) Key() string {
	return s.Cluster + "/" + s.Namespace + "/" + s.Name
}

func (s Slot) String() string { return s.Key() }

// Empty reports a slot that names nothing.
func (s Slot) Empty() bool { return s.Cluster == "" && s.Namespace == "" && s.Name == "" }

// Promotion moves an ALREADY-BUILT artifact to a slot. It is the act that happens every day.
//
// Building and promoting are different decisions. A build produces an artifact; a promotion
// exposes it to a population. Rebuilding per environment breaks the chain of evidence — the
// thing that passed testing is then not the thing that ships, however identical the source.
//
// So a promotion carries a DIGEST, never a tag or a version range. A tag is a moving pointer:
// approving one approves whatever it points at tomorrow, which is precisely the gap between
// "they approved deploy v7" and "v8 shipped".
type Promotion struct {
	Team string `json:"team"`
	App  string `json:"app"`
	// Repository is where the artifact lives, e.g. "harbor/payments/api". Without it a digest
	// is not pullable: a container reference is repository@digest, and a digest alone names an
	// artifact nobody can fetch.
	Repository string `json:"repository"`
	// Digest is the immutable artifact reference, e.g. "sha256:9c4f…".
	Digest string `json:"digest"`
	To     Slot   `json:"to"`
	// Tier is the consequence of exposing this artifact HERE. The same digest is a playground
	// change in one slot and a production change in another; the artifact did not change, the
	// blast radius did.
	Tier Tier `json:"tier"`
}

// Validate refuses a promotion that could not bind an approval to an artifact.
func (p Promotion) Validate() error {
	if p.Team == "" || p.App == "" {
		return fmt.Errorf("promotion: team and app are required")
	}
	if p.Repository == "" {
		return fmt.Errorf("promotion: a repository is required — a digest alone is not a " +
			"pullable reference, so an adapter could not deploy it")
	}
	if strings.ContainsAny(p.Repository, ":@") {
		return fmt.Errorf(
			"promotion: repository %q must not carry a tag or digest — the digest is a separate "+
				"field so the approval binds to it explicitly", p.Repository)
	}
	if p.To.Cluster == "" || p.To.Name == "" {
		return fmt.Errorf("promotion: a target slot needs at least a cluster and a name")
	}
	if !strings.HasPrefix(p.Digest, "sha256:") {
		// A tag here would make the approval meaningless: it would bind to a pointer that
		// somebody else can move after the fact.
		return fmt.Errorf(
			"promotion: %q is not a digest — an approval must bind to an immutable artifact, "+
				"and a tag can be repointed after it is approved", p.Digest)
	}
	if !p.Tier.valid() {
		return fmt.Errorf("promotion: tier %q is not one of dev, shared, prod", p.Tier)
	}
	return nil
}

// AsChange turns a promotion into the one change the door rules on and an adapter applies.
func (p Promotion) AsChange(floor Floor, runtime Runtime) (DesiredItem, error) {
	if err := p.Validate(); err != nil {
		return DesiredItem{}, err
	}
	byTier, ok := floor.App[runtime]
	if !ok {
		return DesiredItem{}, fmt.Errorf("promotion: runtime %q is not configured", runtime)
	}
	limits, ok := byTier[p.Tier]
	if !ok {
		return DesiredItem{}, fmt.Errorf("promotion: no %q floor for tier %q", runtime, p.Tier)
	}
	return DesiredItem{
		Asset: "app", Kind: "deployment", Name: p.To.Name,
		Slot: p.To, Tier: p.Tier, Gate: floor.GateFor(p.Tier),
		Cluster: p.To.Cluster, Limits: limits,
		// BOTH: the repository so an adapter can build a pullable reference, and the digest so
		// the approval binds to an artifact nobody can repoint. Carrying only the digest is how
		// the promotion path was modelled, unit-tested, and unable to deploy.
		Image: p.Repository, Digest: p.Digest, Runtime: string(runtime),
	}, nil
}
