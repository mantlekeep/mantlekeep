package policy

import (
	"os"
	"strings"
)

// NamespaceEnv names the policy namespace a deployment decides under. It prefixes the
// PolicyID written onto every audit record — "<namespace>.rbac", "<namespace>.failsafe".
//
// It is configurable because that value is PERMANENT: a launcher can hide an environment
// prefix and a config file can be renamed, but a decision already on the hash-chain cannot
// be relabelled. A white-labelled deployment whose ledger says "mantlekeep.rbac" has the
// framework's name in the one place it can never be taken out of.
const NamespaceEnv = "MANTLEKEEP_POLICY_NAMESPACE"

// brandEnv is the fallback: a product that has already declared its brand should not have
// to declare the same name twice.
const brandEnv = "MANTLEKEEP_BRAND_NAME"

const defaultNamespace = "mantlekeep"

// namespace resolves the policy namespace: the explicit setting, else the brand name
// reduced to an identifier, else the framework's own name.
//
// Read per call rather than cached, so a test can set it and a launcher can apply a brand
// after this package is loaded. The cost is a map lookup against a decision that already
// evaluates policy.
func namespace() string {
	if explicit := strings.TrimSpace(os.Getenv(NamespaceEnv)); explicit != "" {
		return identifier(explicit)
	}
	if brand := strings.TrimSpace(os.Getenv(brandEnv)); brand != "" {
		return identifier(brand)
	}
	return defaultNamespace
}

// identifier reduces a display name to something safe in a PolicyID: lowercase, with
// runs of non-alphanumerics collapsed to a single dash. "Acme Platform" becomes
// "acme-platform". An empty result falls back rather than producing a nameless policy.
func identifier(displayName string) string {
	var b strings.Builder
	lastWasDash := false
	for _, r := range strings.ToLower(displayName) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastWasDash = false
		case !lastWasDash && b.Len() > 0:
			b.WriteByte('-')
			lastWasDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return defaultNamespace
	}
	return out
}

// policyID returns the identifier written onto an audit record for this decision kind.
func policyID(kind string) string { return namespace() + "." + kind }
