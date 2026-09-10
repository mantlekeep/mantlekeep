package main

import (
	"os"
	"path/filepath"
	"testing"
)

// javaFile writes one Java source file into a temp dir and returns its path.
func javaFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.java")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the probe: %v", err)
	}
	return path
}

// The rule, and the exemptions it must honour.
//
// These lock behaviour that was previously only ever confirmed by hand, once. A checker whose
// detection silently stops working keeps printing a clean gate — which is worse than no gate,
// because it stops people looking.
func TestWhatCountsAsAnUndocumentedEmptyBody(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		reported bool
	}{
		{
			name:     "a bare empty body is reported",
			source:   "public class Probe {\n    void leak() {\n    }\n}\n",
			reported: true,
		},
		{
			name:     "an empty body on one line is reported",
			source:   "public class Probe {\n    public void close() {}\n}\n",
			reported: true,
		},
		{
			name: "a comment saying why is the documented fix, and clears it",
			source: "public class Probe {\n    public void close() {\n" +
				"        // Nothing to release: this holds no handle.\n    }\n}\n",
			reported: false,
		},
		{
			name:     "an empty private constructor is exempt, because S1118 requires it",
			source:   "public final class Probe {\n    private Probe() {\n    }\n}\n",
			reported: false,
		},
		{
			name:     "a body with a statement is not empty",
			source:   "public class Probe {\n    void act() {\n        run();\n    }\n}\n",
			reported: true == false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			found := emptyBodiesIn(javaFile(t, testCase.source))
			if testCase.reported && len(found) == 0 {
				t.Fatalf("expected a finding, got none:\n%s", testCase.source)
			}
			if !testCase.reported && len(found) != 0 {
				t.Fatalf("expected no finding, got %d:\n%s", len(found), testCase.source)
			}
		})
	}
}

// The shapes a line-at-a-time regex gets wrong, each of which this detector got wrong once.
//
// All three were found by review, not by this file — which is the argument for the file. They are
// asserted rather than described, because the cost of each is asymmetric: a false negative lets a
// release through that a downstream scan then blocks, and a false positive teaches people to
// switch the checker off.
func TestTheShapesThatBrokeThisDetectorBefore(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		reported bool
		why      string
	}{
		{
			name:     "a parameter type named Record is still a method",
			source:   "public class Probe {\n    void save(Record record) {\n    }\n}\n",
			reported: true,
			why:      "matching the keyword anywhere on the line skipped a real method",
		},
		{
			name:     "a signature split across lines is still a method",
			source:   "public class Probe {\n    void wide(\n        String a) {\n    }\n}\n",
			reported: true,
			why:      "a line-at-a-time scan sees neither half as a declaration",
		},
		{
			name:     "an annotated constructor is still a constructor",
			source:   "public class Probe {\n    @Inject Probe() {\n    }\n}\n",
			reported: false,
			why:      "anchoring the exemption on the modifier let an annotation defeat it",
		},
		{
			name:     "a static initialiser is not a method",
			source:   "public class Probe {\n    static {\n    }\n}\n",
			reported: false,
			why:      "an initialiser has no name and no signature to report",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			found := emptyBodiesIn(javaFile(t, testCase.source))
			if testCase.reported && len(found) == 0 {
				t.Fatalf("expected a finding (%s):\n%s", testCase.why, testCase.source)
			}
			if !testCase.reported && len(found) != 0 {
				t.Fatalf("expected no finding (%s), got %d:\n%s", testCase.why, len(found), testCase.source)
			}
		})
	}
}
