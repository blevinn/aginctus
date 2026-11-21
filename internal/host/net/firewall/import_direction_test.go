package firewall

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The host firewall owner must not depend on gateway/product policy or
// Cobra/CLI execution. Keep this check colocated with the domain it protects.
func TestHostFirewallImportDirection(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Clean(entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range file.Imports {
				path, err := strconv.Unquote(item.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{
					"github.com/blevinn/aginctus/internal/cli",
					"github.com/blevinn/aginctus/internal/gateway",
					"github.com/blevinn/aginctus/internal/gatewaycontrol",
					"github.com/blevinn/aginctus/plugins/",
				} {
					if path == strings.TrimSuffix(forbidden, "/") || strings.HasPrefix(path, strings.TrimSuffix(forbidden, "/")+"/") {
						t.Fatalf("host firewall package imports product/CLI owner %q", path)
					}
				}
			}
		})
	}
}
