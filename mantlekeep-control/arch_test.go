package mantlekeep

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestCoreDependsOnStdlibOnly is the HEXAGON GUARD. The core (the ports + domain
// types in this package) is the center of the hexagon: it must depend on NOTHING
// but the standard library. No adapter, no third-party, no driven-side concern may
// leak into the center. Adapters depend on the core — never the reverse.
//
// A non-stdlib import has a dot in its first path segment ("go.etcd.io/bbolt",
// "mantlekeep.dev/opa/opa"); stdlib does not ("context", "crypto/sha256").
// This is the invariant that keeps the ports swappable for a downstream host: if
// the core stayed pure, any adapter can be replaced without touching it.
func TestCoreDependsOnStdlibOnly(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			first := path
			if i := strings.IndexByte(path, '/'); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q — the core hexagon must depend on stdlib only "+
					"(no adapters, no third-party). Put this behind a port instead.", name, path)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no core source files scanned — guard would pass vacuously")
	}
}

// TestContractVersionSet guards that the ports carry a version downstream can pin.
func TestContractVersionSet(t *testing.T) {
	if strings.Count(ContractVersion, ".") != 2 {
		t.Fatalf("ContractVersion must be semver (x.y.z), got %q", ContractVersion)
	}
}
