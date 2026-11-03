// Package archtest enforces the dependency rules that keep modules independent.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const root = "cortex/backend/internal/"

// imports lists the internal packages imported by each non-test Go file under dir.
func imports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, root) {
				out[path] = append(out[path], strings.TrimPrefix(p, root))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func owner(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "modules" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func TestModulesDependOnlyOnSharedLayers(t *testing.T) {
	for file, deps := range imports(t, "../modules") {
		self := owner(file)
		if filepath.Base(filepath.Dir(file)) == "modules" {
			continue // registry.go composes modules
		}
		for _, dep := range deps {
			switch {
			case strings.HasPrefix(dep, "platform/"), strings.HasPrefix(dep, "domain/"), strings.HasPrefix(dep, "repo/"), dep == "store", dep == "knowledge", dep == "objects":
			default:
				t.Errorf("%s (module %s) imports %s; modules may only use platform, domain and repo", file, self, dep)
			}
		}
	}
}

func TestDomainStaysPure(t *testing.T) {
	for file, deps := range imports(t, "../domain") {
		for _, dep := range deps {
			if !strings.HasPrefix(dep, "domain/") && dep != "labrange" {
				t.Errorf("%s imports %s; domain packages must be pure logic", file, dep)
			}
		}
	}
}

func TestPlatformDoesNotKnowModules(t *testing.T) {
	for file, deps := range imports(t, "../platform") {
		for _, dep := range deps {
			if strings.HasPrefix(dep, "modules") || strings.HasPrefix(dep, "api") {
				t.Errorf("%s imports %s; platform must not depend on modules or the router", file, dep)
			}
		}
	}
}
