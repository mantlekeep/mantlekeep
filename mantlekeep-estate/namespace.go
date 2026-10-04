package estate

import (
	"fmt"
	"regexp"
	"strings"
)

// NamespacePattern names the namespace an app lands in on one cluster. Empty means "{owns}".
//
// Placeholders: {owns} (the manifest's owns) and {env} (the cluster's environment). A literal with
// neither is valid: every team then shares one namespace, kept apart by the deployment name.
//
// Fixed once anything is deployed: a changed pattern moves every slot, so each app reads as drift.
type NamespacePattern string

const (
	placeholderOwns = "{owns}"
	placeholderEnv  = "{env}"
	// maxNamespace is Kubernetes' limit for a namespace name (a DNS-1123 label).
	maxNamespace = 63
)

var namespaceLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Validate refuses a pattern naming a placeholder other than {owns} and {env}.
func (p NamespacePattern) Validate() error {
	rest := strings.NewReplacer(placeholderOwns, "", placeholderEnv, "").Replace(string(p))
	if strings.ContainsAny(rest, "{}") {
		return fmt.Errorf("namespace pattern %q: only %s and %s are known", p, placeholderOwns, placeholderEnv)
	}
	return nil
}

// Render returns the namespace for owns on a cluster of env, refused unless a valid label.
func (p NamespacePattern) Render(owns, env string) (string, error) {
	if p == "" {
		return owns, nil
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	namespace := strings.NewReplacer(placeholderOwns, owns, placeholderEnv, env).Replace(string(p))
	if len(namespace) > maxNamespace || !namespaceLabel.MatchString(namespace) {
		return "", fmt.Errorf("namespace pattern %q gives %q, not a Kubernetes namespace name "+
			"(lowercase letters, digits and '-', at most %d)", p, namespace, maxNamespace)
	}
	return namespace, nil
}
