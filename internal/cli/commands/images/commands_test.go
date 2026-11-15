package imagescmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Test the command owner directly: parsing and confirmation must finish
// before the loader or a privileged/scoped callback is invoked.
func TestImageRegistrationRejectsUnsafeSyntaxBeforeCapabilities(t *testing.T) {
	for _, args := range [][]string{
		{"images", "import", "--project=aginctus", "x", "/tmp"},
		{"images", "import-owned", "--project=aginctus", "--owner=user", "x", "/tmp"},
		{"images", "owner-audit", "--project=aginctus", "x"},
		{"images", "replace-plan", "--project=aginctus", "--owner=bad", "x", "/tmp"},
		{"images", "list", "--confirm-create"},
		{"images", "update", "x"},
		{"images", "import", "--confirm-create", "x", "/tmp"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(args)
			loaded, called, status := 0, 0, 0
			Register(root, &stdout, &stderr, func() (*config.Config, error) {
				loaded++
				return &config.Config{}, nil
			}, &status, Handlers{
				Legacy: func(context.Context, []string, io.Writer, io.Writer) int {
					called++
					return 0
				},
				Configured: func(context.Context, []string, io.Writer, io.Writer, *config.Config) int {
					called++
					return 0
				},
			})
			if err := root.Execute(); err == nil {
				t.Fatal("invalid image syntax unexpectedly accepted")
			}
			if loaded != 0 || called != 0 || status != 0 || stdout.Len() != 0 {
				t.Fatalf("invalid syntax contacted capabilities: load=%d handlers=%d status=%d stdout=%q", loaded, called, status, stdout.String())
			}
		})
	}
}

func TestImageRegistrationBindsExactArgumentsAndFreshFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		target string
		want []string
	}{
		{"legacy list", []string{"images", "list"}, "legacy", []string{"list"}},
		{"scoped list", []string{"images", "list", "--project", "aginctus"}, "configured", []string{"list", "--project", "aginctus"}},
		{"legacy update", []string{"images", "update", "alias", "/tmp"}, "legacy", []string{"update", "alias", "/tmp"}},
		{"create only", []string{"images", "import", "--project=aginctus", "--confirm-create", "alias", "/tmp"}, "configured", []string{"import", "--project", "aginctus", "alias", "/tmp", "--confirm-create"}},
		{"claimed owner", []string{"images", "import-owned", "--project=aginctus", "--owner=user", "--confirm-create", "alias", "/tmp"}, "configured", []string{"import-owned", "--project", "aginctus", "--owner", "user", "alias", "/tmp", "--confirm-create"}},
		{"read only owner audit", []string{"images", "owner-audit", "--project=aginctus", "--owner=system", "alias"}, "configured", []string{"owner-audit", "--project", "aginctus", "--owner", "system", "alias"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(tc.args)
			status, loads, calls := 0, 0, 0
			var target string
			var received []string
			record := func(kind string, args []string) int {
				calls++
				target = kind
				received = append([]string(nil), args...)
				return 17
			}
			Register(root, &stdout, &stderr, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, Handlers{
				Legacy: func(_ context.Context, args []string, _, _ io.Writer) int {
					return record("legacy", args)
				},
				Configured: func(_ context.Context, args []string, _, _ io.Writer, _ *config.Config) int {
					return record("configured", args)
				},
			})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if loads != 1 || calls != 1 || status != 17 || target != tc.target || !reflect.DeepEqual(received, tc.want) {
				t.Fatalf("load=%d calls=%d status=%d target=%q args=%q, want %s %q", loads, calls, status, target, received, tc.target, tc.want)
			}
		})
	}
}
