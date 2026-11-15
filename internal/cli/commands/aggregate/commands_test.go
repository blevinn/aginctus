package aggregatecmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Aggregation flags are validated by the owning Cobra package, not by
// root CLI global test seams.
func TestAggregateRegistrationRejectsSyntaxBeforeCapabilities(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "--invalid"},
		{"apply", "--render", "--from", "sample.yaml"},
		{"apply", "unexpected"},
		{"remove", "--force"},
		{"status", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errs bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&out)
			root.SetErr(&errs)
			root.SetArgs(args)
			loads, calls, status := 0, 0, 0
			record := func() int { calls++; return 0 }
			Register(root, &out, &errs, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, Handlers{
				Apply: func(context.Context, bool, string, io.Writer, io.Writer, *config.Config) int { return record() },
				Remove: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record() },
				Status: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record() },
			})
			if err := root.Execute(); err == nil {
				t.Fatal("invalid aggregate syntax was accepted")
			}
			if loads != 0 || calls != 0 || status != 0 || out.Len() != 0 {
				t.Fatalf("invalid syntax used capabilities: load=%d calls=%d status=%d out=%q", loads, calls, status, out.String())
			}
		})
	}
}

func TestAggregateExplicitEmptyPlanPathIsOperationError(t *testing.T) {
	var out, errs bytes.Buffer
	root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs([]string{"apply", "--from="})
	status, loads, calls := 0, 0, 0
	Register(root, &out, &errs, func() (*config.Config, error) {
		loads++
		return &config.Config{}, nil
	}, &status, Handlers{
		Apply: func(context.Context, bool, string, io.Writer, io.Writer, *config.Config) int {
			calls++
			return 0
		},
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if status != 1 || loads != 0 || calls != 0 || out.Len() != 0 || !strings.Contains(errs.String(), "regular file path") {
		t.Fatalf("empty --from: status=%d loads=%d calls=%d stdout=%q stderr=%q", status, loads, calls, out.String(), errs.String())
	}
}

func TestAggregateRegistrationDispatchesTypedRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"render", []string{"apply", "--render"}, "apply:true:"},
		{"from file", []string{"apply", "--from", "plan.yaml"}, "apply:false:plan.yaml"},
		{"remove", []string{"remove"}, "remove"},
		{"status", []string{"status"}, "status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errs bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&out)
			root.SetErr(&errs)
			root.SetArgs(tc.args)
			loads, calls, status, action := 0, 0, 0, ""
			record := func(value string) int { calls++; action = value; return 23 }
			Register(root, &out, &errs, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, Handlers{
				Apply: func(_ context.Context, render bool, from string, _, _ io.Writer, _ *config.Config) int {
					return record(fmt.Sprintf("apply:%t:%s", render, from))
				},
				Remove: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record("remove") },
				Status: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record("status") },
			})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if status != 23 || loads != 1 || calls != 1 || action != tc.want {
				t.Fatalf("status=%d loads=%d calls=%d action=%q, want %q", status, loads, calls, action, tc.want)
			}
		})
	}
}
