// Rule S1186 — a method with an empty body.
//
// Sonar reports this as a CRITICAL code smell in Java, and three of them blocked a downstream
// scan: `close()` on in-memory test doubles that genuinely have nothing to release. The rule is
// satisfied by an explanatory comment INSIDE the body, and that is the correct fix here — the
// suggested alternative, `throw new UnsupportedOperationException`, would fail every test that
// closes the double through try-with-resources or a Spring context shutdown.
//
// Empty is often right. Silently empty never is: the reader cannot tell "nothing to do" from
// "nobody finished this".
//
// Constructors are exempt, because Sonar exempts them: an empty private constructor is what
// S1118 REQUIRES of a utility class, so reporting it would put this checker in direct conflict
// with the profile it exists to anticipate. A constructor is recognised by having no return
// type -- `private JsonText() {` is a constructor, `private void reset() {` is a method.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A declaration line ending in `{`, or a whole method on one line ending in `{}`.
var (
	methodOpensBody   = regexp.MustCompile(`\b\w+\s*\([^)]*\)\s*(throws\s[\w,.\s]+)?\{\s*$`)
	methodEmptyInline = regexp.MustCompile(`\b\w+\s*\([^)]*\)\s*(throws\s[\w,.\s]+)?\{\s*\}\s*$`)
	// Lines that end in `{` without being a method: types, control flow, lambdas, initialisers.
	notAMethod = regexp.MustCompile(`\b(class|interface|enum|record|new|if|else|for|while|switch|try|catch|finally|do|synchronized)\b|->\s*\{\s*$`)
	// A constructor: optional access modifier, then the name, then the parameter list. No
	// return type sits between them, which is exactly what distinguishes it from a method.
	constructor = regexp.MustCompile(`^(public\s+|protected\s+|private\s+)?[A-Z]\w*\s*\(`)
)

// reportEmptyBodies walks the roots looking for Java methods whose body holds no statement and
// no comment saying why.
func reportEmptyBodies(roots []string) bool {
	found := false
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".java") || isBuildOutput(path) {
				return nil
			}
			for _, line := range emptyBodiesIn(path) {
				fmt.Printf("%s:%d: S1186 method body is empty and says nothing about why: %s\n",
					path, line.number, line.text)
				found = true
			}
			return nil
		})
	}
	return found
}

// isBuildOutput keeps compiled and generated trees out of the report; Sonar does not scan them.
func isBuildOutput(path string) bool {
	for _, dir := range []string{"/target/", "/build/", "/.gradle/", "/node_modules/"} {
		if strings.Contains(path, dir) {
			return true
		}
	}
	return false
}

type emptyBody struct {
	number int
	text   string
}

// emptyBodiesIn reads one file and returns each empty method body in it.
//
// A body counts as documented if it holds ANY comment, which is exactly Sonar's rule — a comment
// is the author saying the emptiness is deliberate.
func emptyBodiesIn(path string) []emptyBody {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var (
		lines []string
		found []emptyBody
	)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if notAMethod.MatchString(line) || constructor.MatchString(line) {
			continue
		}
		if methodEmptyInline.MatchString(line) {
			found = append(found, emptyBody{number: i + 1, text: line})
			continue
		}
		if !methodOpensBody.MatchString(line) {
			continue
		}
		if bodyIsEmpty(lines, i+1) {
			found = append(found, emptyBody{number: i + 1, text: line})
		}
	}
	return found
}

// bodyIsEmpty reports whether everything between the opening line and the closing brace is
// blank. A comment counts as content: that is the documented-on-purpose case the rule allows.
func bodyIsEmpty(lines []string, start int) bool {
	for i := start; i < len(lines); i++ {
		body := strings.TrimSpace(lines[i])
		if body == "}" {
			return true
		}
		if body != "" {
			return false
		}
	}
	return false
}
