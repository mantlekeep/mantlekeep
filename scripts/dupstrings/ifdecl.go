package main

// The other rule that has blocked a release: an `if` whose init statement declares a variable the
// condition then uses exactly once.
//
//	if err := f(); err != nil          // fine — err is used, and scoping it is the point
//	if ok := check(); ok { }           // flagged — the binding names nothing the call did not
//
// Sonar reports it as "remove this unnecessary variable declaration and use the expression
// directly in the condition". Minor, but it fails a gate that requires zero.
//
// Only reported when the variable appears ONCE in the condition and NOWHERE in the body — then
// the name is pure ceremony. A variable used twice, or used inside the branch, is earning its
// keep and is left alone.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func reportIfDecls(roots []string, includeTests bool) bool {
	found := false
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !includeTests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if reportIfDeclsIn(path) {
				found = true
			}
			return nil
		})
	}
	return found
}

// reportIfDeclsIn walks one file's if-statements.
func reportIfDeclsIn(path string) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		name, ok := needlessBinding(stmt)
		if !ok {
			return true
		}
		fmt.Printf("   S1854  %s:%d\n          %q is declared and used once — put the expression in the condition\n",
			path, fset.Position(stmt.Pos()).Line, name)
		found = true
		return true
	})
	return found
}

// needlessBinding reports the name an if-statement declares for no reason.
//
// Only when the variable appears ONCE in the condition and NOWHERE in either branch — then the
// name is pure ceremony. Used twice, or used inside a branch, and it is earning its keep.
func needlessBinding(stmt *ast.IfStmt) (string, bool) {
	if stmt.Init == nil {
		return "", false
	}
	assign, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 {
		return "", false
	}
	name, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || name.Name == "_" {
		return "", false
	}
	if uses(stmt.Cond, name.Name) != 1 || uses(stmt.Body, name.Name) > 0 {
		return "", false
	}
	if stmt.Else != nil && uses(stmt.Else, name.Name) > 0 {
		return "", false
	}
	return name.Name, true
}

// uses counts how many times an identifier appears in a subtree.
func uses(node ast.Node, name string) int {
	n := 0
	ast.Inspect(node, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && id.Name == name {
			n++
		}
		return true
	})
	return n
}
