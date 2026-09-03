package catalog_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestCatalogImportsEveryDashboardDomain makes a forgotten domain loud.
//
// catalog.go composes the registry by hand, one append per domain. Adding a
// package under internal/dashboards and forgetting to wire it here is the one
// mistake in this repo with no signal at all: build, vet, lint, the whole test
// suite and `make diff` all stay green, because the boards simply do not exist
// as far as anything downstream can tell. Nothing renders, so nothing can be
// wrong.
//
// The alternative — discovering domains by reflection or an init() registry —
// trades this one test for import cycles and an ordering that no longer reads
// off the page. Explicit composition is worth keeping; it just needs a test.
func TestCatalogImportsEveryDashboardDomain(t *testing.T) {
	t.Parallel()

	const domainRoot = "../dashboards"

	entries, err := os.ReadDir(domainRoot)
	if err != nil {
		t.Fatalf("read %s: %v", domainRoot, err)
	}

	imported := catalogImports(t)

	var domains int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// A directory holding no Go package is scaffolding, not a domain.
		matches, err := filepath.Glob(filepath.Join(domainRoot, entry.Name(), "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			continue
		}
		domains++

		want := "github.com/duynhlab/obs-as-code/internal/dashboards/" + entry.Name()
		if !imported[want] {
			t.Errorf("internal/dashboards/%s exists but catalog.go does not import it; "+
				"add %s.Dashboards() to the registry in catalog.go", entry.Name(), entry.Name())
		}
	}

	if domains == 0 {
		t.Fatal("found no dashboard domains, so this test proved nothing")
	}
}

func catalogImports(t *testing.T) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "catalog.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse catalog.go: %v", err)
	}

	out := make(map[string]bool, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("import path %s: %v", spec.Path.Value, err)
		}
		out[path] = true
	}
	return out
}

// TestEveryImportedDomainContributesDashboards catches the other half: a domain
// wired into the imports but never appended, which the import check above would
// call satisfied.
func TestEveryImportedDomainContributesDashboards(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "catalog.go", nil, 0)
	if err != nil {
		t.Fatalf("parse catalog.go: %v", err)
	}

	called := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Dashboards" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok {
			called[pkg.Name] = true
		}
		return true
	})

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		const prefix = "github.com/duynhlab/obs-as-code/internal/dashboards/"
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		pkg := strings.TrimPrefix(path, prefix)
		if !called[pkg] {
			t.Errorf("catalog.go imports %s but never calls %s.Dashboards(); "+
				"its boards would silently not exist", path, pkg)
		}
	}
}
