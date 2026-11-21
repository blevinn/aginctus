package clihelp

import (
    "bytes"
    "strings"
    "testing"

    "github.com/spf13/cobra"
)

func TestBasicHelpNavigatesOnlyDirectChildren(t *testing.T) {
    root := &cobra.Command{Use: "aginctus", Short: "Manage environments"}
    root.PersistentFlags().String("configuration-file", "", "Private configuration path")
    group := &cobra.Command{Use: "gateway", Short: "Manage gateways"}
    nested := &cobra.Command{Use: "egress", Short: "Manage egress"}
    leaf := &cobra.Command{Use: "plan", Short: "Preview an egress change", RunE: func(*cobra.Command, []string) error { t.Fatal("help must not run commands"); return nil }}
    leaf.Flags().String("ranges", "", "Address ranges")
    nested.AddCommand(leaf)
    group.AddCommand(nested)
    root.AddCommand(group)

    var basic bytes.Buffer
    WriteCobraHelp(root, &basic)
    output := basic.String()
    if !strings.Contains(output, "gateway") || strings.Contains(output, "plan") || strings.Contains(output, "--configuration-file") {
        t.Fatalf("unexpected root help: %s", output)
    }
    if lines := strings.Count(output, "\n"); lines > 35 {
        t.Fatalf("basic help exceeded 35 lines: %d", lines)
    }

    basic.Reset()
    WriteCobraHelp(group, &basic)
    if !strings.Contains(basic.String(), "egress") || strings.Contains(basic.String(), "plan") {
        t.Fatalf("group help must list immediate children only: %s", basic.String())
    }
}

func TestFullReferencePreservesDescendantSyntaxAndFlags(t *testing.T) {
    root := &cobra.Command{Use: "aginctus"}
    group := &cobra.Command{Use: "gateway"}
    leaf := &cobra.Command{Use: "plan TARGET", Run: func(*cobra.Command, []string) {}}
    leaf.Flags().Bool("dry-run", false, "Preview changes")
    group.AddCommand(leaf)
    root.AddCommand(group)

    var out bytes.Buffer
    WriteCobraReference(root, &out)
    for _, want := range []string{"Subcommand syntax (derived from registered Cobra commands):", "aginctus gateway plan TARGET", "--dry-run"} {
        if !strings.Contains(out.String(), want) {
            t.Errorf("reference missing %q: %s", want, out.String())
        }
    }
}
