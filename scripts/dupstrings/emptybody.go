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
	//
	// Anchored to the START of the declaration, because these words are also ordinary
	// identifiers: `void save(Record record) {` is a method, and a keyword-anywhere match
	// silently skipped it — a false negative that a downstream scan still blocks a release on.
	notAMethod = regexp.MustCompile(`^(class|interface|enum|record|new|if|else|for|while|switch|try|catch|finally|do|synchronized)\b`)
	// A lambda body, wherever it appears on the line.
	lambdaBody = regexp.MustCompile(`->\s*\{\s*$`)
	// A constructor: the declaration's own name, then the parameter list, with NO return type
	// between them — which is exactly what distinguishes it from a method.
	//
	// Matched AFTER leading annotations and modifiers are stripped. Anchoring on the modifier
	// alone was wrong: `@Inject Probe() {` and `static Probe() {` both defeated it, so an
	// annotated constructor was reported as an empty method — a false positive on a shape
	// S1118 actively requires.
	constructor = regexp.MustCompile(`^[A-Z]\w*\s*\(`)
	// Annotations and modifiers that may precede a declaration, stripped before classifying it.
	leadingNoise = regexp.MustCompile(`^\s*(@\w+(\([^)]*\))?\s+|(public|protected|private|static|final|abstract|synchronized|native|default|strictfp)\s+)+`)
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

	joined := joinWrappedSignatures(lines)

	for i, raw := range joined {
		line := strings.TrimSpace(raw)
		// Classify the declaration with annotations and modifiers removed, so `@Override`,
		// `public static` and friends do not change what a line IS.
		declaration := leadingNoise.ReplaceAllString(line, "")
		if lambdaBody.MatchString(line) || notAMethod.MatchString(declaration) || constructor.MatchString(declaration) {
			continue
		}
		if methodEmptyInline.MatchString(line) {
			found = append(found, emptyBody{number: i + 1, text: line})
			continue
		}
		if !methodOpensBody.MatchString(line) {
			continue
		}
		if bodyIsEmpty(joined, i+1) {
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

// joinWrappedSignatures folds a parameter list split across lines back onto one line.
//
// Java wraps long signatures, and a line-at-a-time scanner sees `void wide(` followed by
// `String a) {` — neither of which is a declaration it recognises. The method is then invisible,
// which is a FALSE NEGATIVE: the downstream scan still reports it and still blocks the release.
//
// The fold is positional: the joined text is written back at the FIRST line's index and the
// continuation lines are blanked, so every reported line number still points at the signature a
// reader would look for.
//
// Parentheses are counted outside string and character literals. That is an approximation — it
// does not track comments or escapes exhaustively — and this file says so rather than implying a
// parser's accuracy.
func joinWrappedSignatures(lines []string) []string {
	folded := make([]string, len(lines))
	copy(folded, lines)

	for i := 0; i < len(folded); i++ {
		if parenDepth(folded[i]) <= 0 {
			continue
		}
		// An unclosed parameter list: pull in following lines until it balances.
		for j := i + 1; j < len(folded) && parenDepth(folded[i]) > 0; j++ {
			folded[i] = strings.TrimRight(folded[i], " \t") + " " + strings.TrimSpace(folded[j])
			folded[j] = ""
		}
	}
	return folded
}

// parenDepth counts unclosed parentheses in a line, ignoring those inside quotes.
func parenDepth(line string) int {
	depth, inString, inChar := 0, false, false
	for index := 0; index < len(line); index++ {
		switch character := line[index]; {
		case (inString || inChar) && character == '\\':
			index++ // skip whatever the backslash escapes
		case inString && character == '"':
			inString = false
		case inChar && character == '\'':
			inChar = false
		case inString || inChar:
			// inside a literal: parentheses there are text, not syntax
		case character == '"':
			inString = true
		case character == '\'':
			inChar = true
		case character == '(':
			depth++
		case character == ')':
			depth--
		}
	}
	return depth
}
