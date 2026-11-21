package clihelp

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRenderBasicBudgetAndNavigation(t *testing.T) {
	root := &cobra.Command{Use: "aginctus", Short: "Manage environments"}
	for _, name := range []string{"environment", "gateway", "workload"} {
		root.AddCommand(&cobra.Command{
			Use: name, Short: "Inspect " + name,
			RunE: func(*cobra.Command, []string) error {
				t.Fatal("help must not run commands")
				return nil
			},
		})
	}
	var out bytes.Buffer
	if err := RenderBasic(root, &out); err != nil { t.Fatal(err) }
	if strings.Count(out.String(), "\n") > BasicHelpMaxLines { t.Fatal("line budget violated") }
	for _, name := range []string{"environment", "gateway", "workload"} {
		if !strings.Contains(out.String(), name) { t.Errorf("missing %q", name) }
	}
}

func TestRenderBasicRejectsOverflowWithoutPartialOutput(t *testing.T) {
	root := &cobra.Command{Use: "aginctus", Short: "Manage environments"}
	for i := 0; i < 40; i++ {
		root.AddCommand(&cobra.Command{Use: fmt.Sprintf("child-%02d", i), Run: func(*cobra.Command, []string) {}})
	}
	var out bytes.Buffer
	if err := RenderBasic(root, &out); err == nil || !strings.Contains(err.Error(), "35") {
		t.Fatalf("expected explicit overflow error, got %v", err)
	}
	if out.Len() != 0 { t.Fatalf("must not emit truncated help: %s", out.String()) }
}

func TestRenderBasicPrioritizesRequiredFlags(t *testing.T) {
	leaf := &cobra.Command{Use: "create NAME", Short: "Create an environment"}
	leaf.Flags().String("project", "", "Incus project")
	if err := leaf.MarkFlagRequired("project"); err != nil { t.Fatal(err) }
	for i := 0; i < 30; i++ {
		leaf.Flags().String(fmt.Sprintf("advanced-%02d", i), "", "Advanced setting")
	}
	leaf.Flags().Bool("dry-run", false, "Preview changes")
	var out bytes.Buffer
	if err := RenderBasic(leaf, &out); err != nil { t.Fatal(err) }
	if !strings.Contains(out.String(), "--project string (required)") {
		t.Fatalf("required flag lost: %s", out.String())
	}
	if strings.Contains(out.String(), "advanced-01") {
		t.Fatalf("advanced flag leaked into compact help: %s", out.String())
	}
	if strings.Count(out.String(), "\n") > BasicHelpMaxLines { t.Fatal("over budget") }
}

func TestRenderBasicRejectsOverWideUnbreakableCommand(t *testing.T) {
	leaf := &cobra.Command{Use: strings.Repeat("a", BasicHelpWidth+1)}
	var out bytes.Buffer
	if err := RenderBasic(leaf, &out); err == nil { t.Fatal("expected width error") }
	if out.Len() != 0 { t.Fatal("renderer wrote invalid partial output") }
}
