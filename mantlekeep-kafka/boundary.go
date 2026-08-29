package kafkagrant

import (
	"fmt"
	"strings"
)

// covers reports whether a PREFIXED ACL on prefix would cover name.
//
// This mirrors the broker's PREFIXED semantics EXACTLY — a plain prefix match, with a
// name equal to the prefix included, because the broker includes it. That exactness is
// the point. Stricter than the broker and this adapter refuses work the team is entitled
// to, which pushes people off the golden path; looser and it creates a resource the
// approved ACL does not cover, which is a permission nobody granted. The one safe
// implementation is the one the broker uses.
//
// An empty prefix would make this true for every name, which is why an empty prefix is
// rejected at validation rather than handled here: a boundary that bounds nothing must
// never be constructible in the first place.
func covers(prefix, name string) bool {
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(name, prefix)
}

// validatePrefix rejects a prefix that cannot bound a namespace.
//
// It checks STRUCTURE only — whether the string can serve as a Kafka resource-name
// prefix at all. Whether this particular team should own this particular prefix is a
// policy question, decided at the door before this package is called.
func validatePrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("%w: prefix must not be empty — an empty prefix covers every topic on the cluster", ErrInvalidGrant)
	}
	if prefix == "*" || strings.Contains(prefix, "*") {
		return fmt.Errorf("%w: prefix %q must not contain a wildcard — a PREFIXED ACL is already a wildcard, "+
			"and \"*\" additionally means ANY resource to the broker", ErrInvalidGrant, prefix)
	}
	return validateNameCharacters(prefix, "prefix")
}

// validateResourceName rejects a topic or group name Kafka itself would reject, so a
// malformed name fails here with a readable reason rather than as a broker error code
// halfway through an apply.
func validateResourceName(name, kind string) error {
	if name == "" {
		return fmt.Errorf("%w: %s name must not be empty", ErrInvalidGrant, kind)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: %s name must not be %q — Kafka reserves it", ErrInvalidGrant, kind, name)
	}
	if len(name) > maxResourceNameLength {
		return fmt.Errorf("%w: %s name %q is %d characters; Kafka allows at most %d",
			ErrInvalidGrant, kind, name, len(name), maxResourceNameLength)
	}
	return validateNameCharacters(name, kind)
}

// maxResourceNameLength is Kafka's limit on a topic name.
const maxResourceNameLength = 249

// validateNameCharacters enforces Kafka's legal resource-name alphabet: ASCII letters,
// digits, period, underscore and hyphen.
func validateNameCharacters(value, kind string) error {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return fmt.Errorf("%w: %s %q contains %q; Kafka allows only [a-zA-Z0-9._-]", ErrInvalidGrant, kind, value, r)
		}
	}
	return nil
}
