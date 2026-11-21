package cli

import (
	"os"
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


// TestRootTestOwnerLedgerPreventsDrift requires an explicit disposition when
// a root test is added or moved. This deliberately does not count files in
// other packages: they own their direct registration/domain unit tests.
func TestRootTestOwnerLedgerPreventsDrift(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "design", "cli-root-test-inventory.md"))
	if err != nil { t.Fatal(err) }
	listed := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") { continue }
		parts := strings.SplitN(line, "`", 3)
		if len(parts) < 3 || !strings.HasSuffix(parts[1], "_test.go") { continue }
		if listed[parts[1]] { t.Errorf("duplicate ownership entry %s", parts[1]) }
		listed[parts[1]] = true
	}
	files, err := filepath.Glob("*_test.go")
	if err != nil { t.Fatal(err) }
	for _, path := range files {
		if !listed[path] { t.Errorf("root test %s has no documented owner", path) }
		delete(listed, path)
	}
	for path := range listed { t.Errorf("stale owner entry for nonexistent root test %s", path) }
}

// TestCLIRootFunctionInjectionSeamsAreAccountedFor prevents new mutable
// function-literal transports from silently appearing in root composition.
// The existing exceptions must each have an explicit owner/disposition in
// cli-root-mutable-seams.md. It does not endorse those exceptions.
func TestCLIRootFunctionInjectionSeamsAreAccountedFor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "design", "cli-root-mutable-seams.md"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) < 4 {
			continue
		}
		file := strings.Trim(fields[1], " `")
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		names := strings.Trim(fields[2], " `")
		allowed[file] = map[string]bool{}
		for _, name := range strings.Split(names, ",") {
			allowed[file][strings.TrimSpace(name)] = true
		}
	}
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			decl, ok := declaration.(*ast.GenDecl)
			if !ok || decl.Tok != token.VAR {
				continue
			}
			for _, entry := range decl.Specs {
				spec, ok := entry.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, value := range spec.Values {
					_, isFunc := value.(*ast.FuncLit)
					if !isFunc {
						continue
					}
					if index >= len(spec.Names) || !allowed[path][spec.Names[index].Name] {
						t.Errorf("%s: package-level mutable function needs a named owner and migration disposition", path)
					}
				}
			}
		}
	}
}

// TestCLIRootTestFunctionsHaveOwnerRecords checks individual test functions,
// not just filenames. The inventory explicitly identifies provisional
// ownership, so moving a function cannot silently drop negative CLI coverage.
func TestCLIRootTestFunctionsHaveOwnerRecords(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "design", "cli-root-test-function-inventory.md"))
	if err != nil {
		t.Fatal(err)
	}
	records := map[string]map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		tokens := strings.Split(line, "`")
		if len(tokens) < 4 || !strings.HasSuffix(tokens[1], "_test.go") || !strings.HasPrefix(tokens[3], "Test") {
			continue
		}
		file, name := tokens[1], tokens[3]
		if records[file] == nil {
			records[file] = map[string]bool{}
		}
		if records[file][name] {
			t.Errorf("duplicate root test owner: %s:%s", file, name)
		}
		records[file][name] = true
	}
	paths, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			if !records[path][fn.Name.Name] {
				t.Errorf("missing root test function owner: %s:%s", path, fn.Name.Name)
			}
			delete(records[path], fn.Name.Name)
		}
	}
	for path, functions := range records {
		for name := range functions {
			t.Errorf("stale test function owner: %s:%s", path, name)
		}
	}
}
