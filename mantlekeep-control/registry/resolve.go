package registry

import (
	"context"
	"fmt"
)

// ResolutionMode is how a consumer's requested version is resolved when it is gone.
type ResolutionMode string

const (
	StrictPin       ResolutionMode = "strict-pin"       // requested version must be live, else error
	DefaultFallback ResolutionMode = "default-fallback" // fall back to the env default, audited
)

// Resolution is the outcome of resolving a requested version. Substituted=true means
// the requested version was unavailable and the env default was used instead — the
// caller MUST record this on the audit chain; a silent swap is a host finding.
type Resolution struct {
	Name        string
	Requested   string
	Resolved    Version
	Substituted bool
	Reason      string
}

// Resolve returns the version a consumer should run. A live published version wins.
// A deprecated version still resolves (with a warning) so old flows keep running.
// A missing or demised version falls back to the env default ONLY under
// DefaultFallback — flagging the substitution for auditing; StrictPin errors instead.
func (r *Registry) Resolve(ctx context.Context, name, requested string, mode ResolutionMode) (Resolution, error) {
	e, ok, err := r.get(ctx, name)
	if err != nil {
		return Resolution{}, err
	}
	if !ok {
		return Resolution{}, fmt.Errorf("registry: %s not found", name)
	}
	if idx := indexOf(e, requested); idx >= 0 {
		v := e.Versions[idx]
		switch v.Status {
		case StatusPublished:
			return Resolution{Name: name, Requested: requested, Resolved: v}, nil
		case StatusDeprecated:
			return Resolution{Name: name, Requested: requested, Resolved: v,
				Reason: fmt.Sprintf("%s@%s is deprecated — plan to migrate", name, requested)}, nil
		}
		// draft / review / demised → not runnable; fall through to the fallback rules.
	}
	if mode != DefaultFallback {
		return Resolution{}, fmt.Errorf("registry: %s@%s is not available (strict pin)", name, requested)
	}
	def := defaultVersion(e)
	if def == nil {
		return Resolution{}, fmt.Errorf("registry: %s@%s unavailable and no default set", name, requested)
	}
	return Resolution{
		Name: name, Requested: requested, Resolved: *def, Substituted: true,
		Reason: fmt.Sprintf("requested %s@%s unavailable; fell back to default %s", name, requested, def.Version),
	}, nil
}

func defaultVersion(e Entry) *Version {
	for i := range e.Versions {
		if e.Versions[i].Default && e.Versions[i].Status == StatusPublished {
			return &e.Versions[i]
		}
	}
	return nil
}
