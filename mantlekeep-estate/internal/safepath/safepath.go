// Package safepath guards a configuration path before it is opened.
package safepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Clean rejects a configuration path that walks upward before it is opened.
//
// The path arrives from the operator's environment rather than from a request, so this is
// defence in depth rather than a boundary: an operator who can set the variable can already
// read the file. It is here because the cost is four lines and the failure it prevents —
// a deployment template composing a path from a value somebody else supplies — is the kind
// that arrives later, in a different file, from a change nobody connected to this one.
//
// It mirrors the core's own config-path guard. Duplicated rather than imported because that
// helper is internal to the core module, and this module carries no dependencies worth
// adding one for.
// Clean rejects a configuration path that walks upward.
func Clean(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config path is empty")
	}
	clean := filepath.Clean(path)
	// After Clean, the only way a path escapes upward is a leading, embedded or trailing
	// ".." segment; a legitimate config path, absolute or relative, never needs one.
	separator := string(os.PathSeparator)
	if clean == ".." ||
		strings.HasPrefix(clean, ".."+separator) ||
		strings.Contains(clean, separator+".."+separator) ||
		strings.HasSuffix(clean, separator+"..") {
		return "", fmt.Errorf("config path %q escapes upward via %q", path, "..")
	}
	return clean, nil
}
