// Command dupstrings reports string literals repeated often enough to trip SonarQube's S1192.
//
// It parses Go rather than grepping it. A regular expression over the raw text matches the gap
// BETWEEN two literals on one line — `Name: "a", Kind: "b"` yields `", Kind: "` — and a checker
// that reports things which are not literals is a checker people switch off.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	min := flag.Int("min", 8, "shortest literal to consider")
	times := flag.Int("times", 3, "how many repeats before it is reported")
	tests := flag.Bool("tests", true, "include _test.go files, as Sonar does")
	flag.Parse()

	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	found := false
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !*tests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if reportFile(path, *min, *times) {
				found = true
			}
			return nil
		})
	}
	if reportIfDecls(roots, *tests) {
		found = true
	}
	if found {
		os.Exit(1)
	}
}

// reportFile prints every over-repeated literal in one file, and says whether it found any.
func reportFile(path string, min, times int) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false // unparseable is a compiler's problem, not this tool's
	}

	counts := map[string]int{}
	first := map[string]token.Position{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil || len(value) < min {
			return true
		}
		// A struct tag is DATA and belongs at every use; so is anything with no space and no
		// format verb, which is a key or an identifier rather than a message.
		if !strings.ContainsAny(value, " %") || strings.Contains(value, ",omitempty") {
			return true
		}
		counts[value]++
		if _, seen := first[value]; !seen {
			first[value] = fset.Position(lit.Pos())
		}
		return true
	})

	over := make([]string, 0, len(counts))
	for value, n := range counts {
		if n >= times {
			over = append(over, value)
		}
	}
	if len(over) == 0 {
		return false
	}
	sort.Slice(over, func(i, j int) bool { return counts[over[i]] > counts[over[j]] })
	for _, value := range over {
		fmt.Printf("   S1192  %s:%d\n          %q appears %d times — define a constant\n",
			path, first[value].Line, value, counts[value])
	}
	return true
}
