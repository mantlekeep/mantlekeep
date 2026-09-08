package main

// Duplicated literals in the languages this tool does not parse — Java, Python, JavaScript.
//
// A regular expression cannot do this. `foo("a", bar, "b")` makes a regex match `", bar, "`, the
// gap BETWEEN two literals, and reporting that as a duplicated string sends somebody looking for
// a constant to extract from a comma. So the file is SCANNED: walk it once, track whether we are
// inside a quote, and respect escapes. After a literal closes, the next quote opens a new one —
// which is exactly what the regex could not know.
//
// This finds S1192 only. Cognitive complexity in these languages needs a real parser, and a wrong
// complexity number is worse than none: it would be argued with rather than acted on. Sonar itself
// is the check for that; this is here to catch the cheap, common one before it gets there.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// scanLanguages are the extensions handled here, with the comment prefix that hides a literal.
var scanLanguages = map[string]string{
	".java": "//",
	".py":   "#",
	".js":   "//",
	".ts":   "//",
}

func reportTextualDuplicates(roots []string, min, times int) bool {
	found := false
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.Contains(path, "/build/") || strings.Contains(path, "/.git/") ||
				strings.Contains(path, "/node_modules/") || strings.Contains(path, "/target/") {
				return nil
			}
			comment, ok := scanLanguages[filepath.Ext(path)]
			if !ok {
				return nil
			}
			if reportTextualIn(path, comment, min, times) {
				found = true
			}
			return nil
		})
	}
	return found
}

func reportTextualIn(path, comment string, min, times int) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	counts, firstLine := countTextualLiterals(string(content), comment, min)

	over := make([]string, 0, len(counts))
	for lit, n := range counts {
		if n >= times {
			over = append(over, lit)
		}
	}
	if len(over) == 0 {
		return false
	}
	sort.Slice(over, func(i, j int) bool { return counts[over[i]] > counts[over[j]] })
	for _, lit := range over {
		fmt.Printf("   S1192  %s:%d\n          %q appears %d times — define a constant\n",
			path, firstLine[lit], lit, counts[lit])
	}
	return true
}

// countTextualLiterals tallies the message-shaped literals in a file's text.
func countTextualLiterals(content, comment string, min int) (map[string]int, map[string]int) {
	counts := map[string]int{}
	firstLine := map[string]int{}
	for i, line := range strings.Split(content, "\n") {
		for _, lit := range literalsIn(line, comment) {
			if len(lit) < min || !strings.ContainsAny(lit, " %") {
				continue
			}
			counts[lit]++
			if _, seen := firstLine[lit]; !seen {
				firstLine[lit] = i + 1
			}
		}
	}
	return counts, firstLine
}

// literalsIn pulls the double-quoted strings out of one line.
//
// Single quotes are skipped deliberately: in Java they are a char, and in Python they are an
// equally valid string — handling only what is unambiguous keeps this from inventing findings.
func literalsIn(line, comment string) []string {
	if idx := strings.Index(line, comment); idx >= 0 && !insideQuote(line, idx) {
		line = line[:idx]
	}
	var out []string
	var current strings.Builder
	inside, escaped := false, false
	for _, r := range line {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && inside:
			escaped = true
		case r == '"':
			if inside {
				out = append(out, current.String())
				current.Reset()
			}
			inside = !inside
		case inside:
			current.WriteRune(r)
		}
	}
	return out
}

// insideQuote reports whether a position sits inside a string, so a // in a URL is not a comment.
func insideQuote(line string, pos int) bool {
	quotes := 0
	for i, r := range line {
		if i >= pos {
			break
		}
		if r == '"' && (i == 0 || line[i-1] != '\\') {
			quotes++
		}
	}
	return quotes%2 == 1
}
