package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Keep dependency direction enforceable as handlers move out of the flat root.
func TestCLICommandAndOperationImportBoundaries(t *testing.T) {
	for _, tc := range []struct {
		root string
		forbidden func(string) bool
	}{
		{
			root: "commands",
			forbidden: func(path string) bool {
				return path == "github.com/blevinn/aginctus/internal/cli" ||
					strings.HasPrefix(path, "github.com/blevinn/aginctus/internal/cli/operations/")
			},
		},
		{
			root: "operations",
			forbidden: func(path string) bool {
				return path == "github.com/blevinn/aginctus/internal/cli" ||
					path == "github.com/spf13/cobra" ||
					strings.HasPrefix(path, "github.com/blevinn/aginctus/internal/cli/commands/")
			},
		},
	} {
		t.Run(tc.root, func(t *testing.T) {
			err := filepath.WalkDir(tc.root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, item := range parsed.Imports {
					name, err := strconv.Unquote(item.Path.Value)
					if err != nil {
						return err
					}
					if tc.forbidden(name) {
						t.Errorf("%s imports forbidden package %q", path, name)
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
