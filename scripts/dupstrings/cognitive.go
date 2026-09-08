package main

// Cognitive complexity, the rule SonarQube reports as S3776.
//
// Implemented here rather than shelling out to a third-party binary. The alternative was
// `go install …@version` in CI, which Sonar itself flags — an install resolves its own
// dependencies with no lock file, so the gate could change behaviour on a morning nobody touched
// this repository. A gate that moves on its own is one people learn to re-run rather than read.
//
// # What it counts
//
// Sonar's definition, and deliberately not cyclomatic complexity: what makes a function hard to
// HOLD IN YOUR HEAD, not how many paths it has.
//
//   +1  for each break in the linear flow: if, for, switch, select, each case, a catch
//   +1  extra for each level of NESTING the break sits inside
//   +1  for each sequence of boolean operators
//   +1  for else / else-if
//
// It is an APPROXIMATION of Sonar's, and the difference goes BOTH ways — measured, not assumed:
//
//	validateFleet, PrecedenceNotices, parseQuantity   identical
//	serve.Run        this reads 16, gocognit 14       closure nesting, which Sonar also charges
//	this main()      this read 15, SONAR SAID 16      Sonar was stricter than this
//
// So treat anything from about 13 up as at risk rather than trusting the number. Passing here is
// evidence, not proof; the only instance whose opinion counts is the one gating the release.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// reportComplexity prints every function over the limit and says whether it found any.
func reportComplexity(roots []string, limit int, includeTests bool) bool {
	found := false
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !includeTests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if reportComplexityIn(path, limit) {
				found = true
			}
			return nil
		})
	}
	return found
}

func reportComplexityIn(path string, limit int) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false
	}
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		score := complexityOf(fn.Body, 0)
		if score <= limit {
			continue
		}
		fmt.Printf("   S3776  %s:%d\n          %s has cognitive complexity %d, over the %d allowed — split it\n",
			path, fset.Position(fn.Pos()).Line, fn.Name.Name, score, limit)
		found = true
	}
	return found
}

// complexityOf scores one node, charging deeper nesting more.
func complexityOf(node ast.Node, nesting int) int {
	score := 0
	ast.Inspect(node, func(n ast.Node) bool {
		add, descend := scoreNode(n, nesting)
		score += add
		return descend
	})
	return score
}

// scoreNode charges one construct and says whether ast.Inspect should keep going.
//
// It returns false for anything it has already recursed into itself: a branch scored twice is a
// function that looks twice as hard to read as it is, and a checker that overstates gets argued
// with rather than acted on.
func scoreNode(n ast.Node, nesting int) (score int, descend bool) {
	switch stmt := n.(type) {
	case *ast.IfStmt:
		return scoreIf(stmt, nesting), false
	case *ast.ForStmt:
		return 1 + nesting + complexityOf(stmt.Body, nesting+1), false
	case *ast.RangeStmt:
		return 1 + nesting + complexityOf(stmt.Body, nesting+1), false
	case *ast.SwitchStmt:
		return 1 + nesting + complexityOf(stmt.Body, nesting+1), false
	case *ast.TypeSwitchStmt:
		return 1 + nesting + complexityOf(stmt.Body, nesting+1), false
	case *ast.SelectStmt:
		return 1 + nesting + complexityOf(stmt.Body, nesting+1), false
	case *ast.CaseClause:
		return 1 + scoreBody(stmt.Body, nesting), false
	case *ast.CommClause:
		return 1 + scoreBody(stmt.Body, nesting), false
	case *ast.FuncLit:
		// A closure's body nests inside whatever holds it. Sonar charges this; some tools do not.
		return complexityOf(stmt.Body, nesting+1), false
	}
	return 0, true
}

// scoreIf charges an if, its else, and the boolean sequences in its condition.
func scoreIf(stmt *ast.IfStmt, nesting int) int {
	score := 1 + nesting + complexityOf(stmt.Body, nesting+1) + booleans(stmt.Cond)
	if stmt.Else != nil {
		score += 1 + complexityOf(stmt.Else, nesting+1) // an else is its own thing to hold
	}
	return score
}

// scoreBody charges a list of statements at the current nesting.
func scoreBody(body []ast.Stmt, nesting int) int {
	score := 0
	for _, s := range body {
		score += complexityOf(s, nesting)
	}
	return score
}

// booleans charges one per SEQUENCE of the same operator, not per operator: `a && b && c` is one
// idea to hold, `a && b || c` is two.
func booleans(expr ast.Expr) int {
	var ops []token.Token
	ast.Inspect(expr, func(n ast.Node) bool {
		if b, ok := n.(*ast.BinaryExpr); ok && (b.Op == token.LAND || b.Op == token.LOR) {
			ops = append(ops, b.Op)
		}
		return true
	})
	sequences := 0
	for i, op := range ops {
		if i == 0 || op != ops[i-1] {
			sequences++
		}
	}
	return sequences
}
