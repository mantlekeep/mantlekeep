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
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				stmt, ok := n.(*ast.IfStmt)
				if !ok || stmt.Init == nil {
					return true
				}
				assign, ok := stmt.Init.(*ast.AssignStmt)
				if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 {
					return true
				}
				name, ok := assign.Lhs[0].(*ast.Ident)
				if !ok || name.Name == "_" {
					return true
				}
				// Used more than once in the condition, or at all in either branch? Then the
				// binding is doing work.
				if uses(stmt.Cond, name.Name) != 1 {
					return true
				}
				if uses(stmt.Body, name.Name) > 0 || (stmt.Else != nil && uses(stmt.Else, name.Name) > 0) {
					return true
				}
				pos := fset.Position(stmt.Pos())
				fmt.Printf("   S1854  %s:%d\n          %q is declared and used once — put the expression in the condition\n",
					path, pos.Line, name.Name)
				found = true
				return true
			})
			return nil
		})
	}
	return found
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
