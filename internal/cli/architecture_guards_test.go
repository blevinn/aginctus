package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const cliImportRoot = "github.com/blevinn/aginctus/internal/cli"

// cliImportForbidden is the executable dependency-direction contract.
// Command packages parse syntax; OS/Incus capabilities and application
// operations are supplied by root composition, never imported from commands.
func cliImportForbidden(role, imported string) bool {
	switch role {
	case "commands":
		return imported == cliImportRoot ||
			imported == cliImportRoot+"/operations" ||
			strings.HasPrefix(imported, cliImportRoot+"/operations/") ||
			imported == "os" || imported == "os/exec" || imported == "syscall"
	case "operations":
		return imported == cliImportRoot ||
			imported == cliImportRoot+"/commands" ||
			strings.HasPrefix(imported, cliImportRoot+"/commands/") ||
			imported == "github.com/spf13/cobra"
	default:
		return false
	}
}

// These examples catch an accidentally weakened guard before a new package
// silently introduces a reverse dependency or direct command-layer I/O.
func TestCLIImportBoundaryRules(t *testing.T) {
	cases := []struct {
		role, imported string
		forbidden      bool
	}{
		{"commands", cliImportRoot, true},
		{"commands", cliImportRoot + "/operations", true},
		{"commands", cliImportRoot + "/operations/gateway", true},
		{"commands", "os", true},
		{"commands", "os/exec", true},
		{"commands", "syscall", true},
		{"commands", "github.com/spf13/cobra", false},
		{"commands", "github.com/blevinn/aginctus/internal/config", false},
		{"operations", cliImportRoot, true},
		{"operations", cliImportRoot + "/commands", true},
		{"operations", cliImportRoot + "/commands/gateway", true},
		{"operations", "github.com/spf13/cobra", true},
		{"operations", "github.com/blevinn/aginctus/internal/config", false},
		{"operations", "os", false},
	}
	for _, tc := range cases {
		if got := cliImportForbidden(tc.role, tc.imported); got != tc.forbidden {
			t.Errorf("%s importing %s: forbidden=%t, want %t", tc.role, tc.imported, got, tc.forbidden)
		}
	}
}

// Cobra command objects and their flags must be built on each invocation.
// Package-level Command variables and init() registration retain state across
// calls and can make --help/syntax depend on host side effects.
func commandPackageGlobalViolations(file *ast.File) []string {
	var violations []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				violations = append(violations, "init() registration")
			}
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				ast.Inspect(value, func(node ast.Node) bool {
					selector, ok := node.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Command" {
						return true
					}
					pkg, ok := selector.X.(*ast.Ident)
					if ok && pkg.Name == "cobra" {
						violations = append(violations, "package-level cobra.Command")
						return false
					}
					return true
				})
			}
		}
	}
	return violations
}

func TestCLICommandPackagesConstructCobraLocally(t *testing.T) {
	err := filepath.WalkDir("commands", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, violation := range commandPackageGlobalViolations(parsed) {
			t.Errorf("%s: %s", path, violation)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLICommandGlobalStateGuardRejectsUnsafeDeclarations(t *testing.T) {
	for _, tc := range []struct {
		src        string
		violations int
	}{
		{"package fixture; import \"github.com/spf13/cobra\"; func Build() *cobra.Command { return &cobra.Command{} }", 0},
		{"package fixture; import \"github.com/spf13/cobra\"; var root = &cobra.Command{}", 1},
		{"package fixture; import \"github.com/spf13/cobra\"; var root *cobra.Command", 1},
		{"package fixture; func init() {}", 1},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", tc.src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(commandPackageGlobalViolations(file)); got != tc.violations {
			t.Errorf("source %q: got %d violations, want %d", tc.src, got, tc.violations)
		}
	}
}
