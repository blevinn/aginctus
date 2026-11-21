package plugins

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/blevinn/aginctus/"

// importBoundaryViolation covers only stable architectural invariants. In
// particular, it does not prohibit transitional product packages that the
// tracked plugin/CLI migrations still own.
func importBoundaryViolation(owner, imported string) bool {
	internal := modulePath + "internal/"
	composition := internal + "plugins/composition"
	concrete := modulePath + "plugins/"
	if owner == "public" || owner == "concrete-plugin" {
		return strings.HasPrefix(imported, internal)
	}
	if owner == "registry-build" {
		return imported == composition || strings.HasPrefix(imported, composition+"/") ||
			strings.HasPrefix(imported, concrete)
	}
	return false
}

func TestOwnershipImportBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		owner, imported string
		forbidden       bool
	}{
		{"public", modulePath + "internal/config", true},
		{"public", modulePath + "pkg/plugin", false},
		{"concrete-plugin", modulePath + "internal/plugins", true},
		{"concrete-plugin", modulePath + "pkg/plugin", false},
		{"registry-build", modulePath + "internal/plugins/composition", true},
		{"registry-build", modulePath + "internal/plugins/composition/sub", true},
		{"registry-build", modulePath + "plugins/herdr", true},
		{"registry-build", modulePath + "internal/plugins/build", false},
		{"registry-build", modulePath + "internal/plugins", false},
		{"registry-build", modulePath + "pkg/plugin", false},
	} {
		if got := importBoundaryViolation(tc.owner, tc.imported); got != tc.forbidden {
			t.Errorf("%s -> %s: got violation=%t, want %t", tc.owner, tc.imported, got, tc.forbidden)
		}
	}
}

// Walk only tracked source domains; skip testdata fixtures and test-only imports
// because external-module fixtures intentionally exercise different import
// contracts and package tests may depend on their owning integration helpers.
// The Go compiler independently rejects cycles in the production graph.
func TestCoreOwnershipImportDirection(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, domain := range []struct {
		path, owner string
	}{
		{"pkg", "public"},
		{"plugins", "concrete-plugin"},
		{"internal/plugins", "registry-build"},
		{"internal/plugins/build", "registry-build"},
	} {
		t.Run(domain.path, func(t *testing.T) {
			start := filepath.Join(root, domain.path)
			err := filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					if entry.Name() == "testdata" {
						return filepath.SkipDir
					}
					// Generic registry checks exclude the application composition
					// point and build subtree (the latter is checked separately).
					if domain.path == "internal/plugins" && path != start {
						return filepath.SkipDir
					}
					return nil
				}
				if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imp := range file.Imports {
					imported, err := strconv.Unquote(imp.Path.Value)
					if err != nil {
						return err
					}
					if importBoundaryViolation(domain.owner, imported) {
						return fmt.Errorf("%s imports forbidden dependency %q", path, imported)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
