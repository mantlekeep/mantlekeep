// Package safeio is the ONE validated door for reading OPERATOR-set config files.
//
// The paths that flow through here are not request input: they come from MANTLEKEEP_*
// environment variables an operator sets when deploying the door — policy grants, config
// layers, the data directory. That is trusted configuration, not attacker-controlled data.
// A security scanner cannot know that from a bare os.ReadFile(path), though, and "trust me"
// is not evidence — so we EARN the trust with a real check rather than suppressing the flag:
// reject an empty path, filepath.Clean it, and reject any path that still walks upward via a
// ".." segment after cleaning. Routing every config read through one function means there is a
// SINGLE, auditable place where a config path becomes a file handle, instead of a dozen bare
// reads each of which a reviewer (and the scanner) must reason about independently. The taint
// is broken by demonstrable validation.
package safeio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CleanConfigPath validates an operator-set config path and returns its cleaned form. It
// rejects an empty path and any path that still walks upward (a ".." segment) after
// filepath.Clean. The returned path is safe to hand to os.ReadFile / os.Stat / os.MkdirAll
// without the caller re-deriving the safety argument.
func CleanConfigPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config path is empty")
	}
	clean := filepath.Clean(path)
	// After Clean, the only way a path escapes upward is a leading, embedded or trailing
	// ".." segment; a legitimate config path (absolute or relative) never needs one.
	sep := string(os.PathSeparator)
	if clean == ".." ||
		strings.HasPrefix(clean, ".."+sep) ||
		strings.Contains(clean, sep+".."+sep) ||
		strings.HasSuffix(clean, sep+"..") {
		return "", fmt.Errorf("config path %q escapes upward via %q", path, "..")
	}
	return clean, nil
}

// ReadConfigFile reads the file named by an operator-set config path after validating it
// with CleanConfigPath. This is the one sink for config-file reads: the single os.ReadFile
// below runs only on a cleaned, traversal-rejected path.
func ReadConfigFile(path string) ([]byte, error) {
	clean, err := CleanConfigPath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(clean) // #nosec G304 -- validated by CleanConfigPath: operator-set config path, cleaned + traversal-rejected
}

// StatConfigPath stats an operator-set config path after validating it with CleanConfigPath,
// so a caller that must branch on file-vs-directory does so on a cleaned path.
func StatConfigPath(path string) (os.FileInfo, error) {
	clean, err := CleanConfigPath(path)
	if err != nil {
		return nil, err
	}
	return os.Stat(clean) // #nosec G703 -- validated by CleanConfigPath: operator-set config path, cleaned + traversal-rejected
}

// EnsureConfigDir creates (if missing) the operator-set directory named by path after
// validating it, using 0o750 perms — owner+group, never world. It returns the cleaned path.
func EnsureConfigDir(path string) (string, error) {
	clean, err := CleanConfigPath(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(clean, 0o750); err != nil { // #nosec G703 -- validated by CleanConfigPath: operator-set config path, cleaned + traversal-rejected
		return "", err
	}
	return clean, nil
}
