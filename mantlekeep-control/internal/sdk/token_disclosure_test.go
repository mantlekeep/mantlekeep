package sdk

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// printsTokenValue matches any attempt to put an execution token's Value into output —
// whole, sliced, or formatted.
var printsTokenValue = regexp.MustCompile(
	`(?i)(Printf|Println|Print|Fprint\w*|slog\.\w+|log\.\w+)\([^)]*\b(tok|token)\w*\.Value`)

// A token's Value is the capability the door issued. Nothing may put it — or a prefix of
// it — into output: stdout in a container is a log aggregator, and a fragment of a live
// credential in a log is a credential in a log.
//
// The intent id identifies the same decision, can be looked up on the chain, and grants
// nothing. It sits beside Value in ExecutionToken precisely so the safe identifier is the
// one within reach.
//
// This walks the source rather than asserting on behaviour because the failure it prevents
// is a line somebody adds later, in a file that does not exist yet.
func TestNoSourceFilePrintsAnExecutionTokenValue(t *testing.T) {
	root := repoRootFrom(t)

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- walking this module's own source
		if readErr != nil {
			return readErr
		}
		for number, line := range strings.Split(string(body), "\n") {
			if printsTokenValue.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+itoa(number+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("an execution token's Value reaches output — print IntentID instead:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// repoRootFrom finds this module's root by walking up to the go.mod.
func repoRootFrom(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the module root")
	return ""
}
