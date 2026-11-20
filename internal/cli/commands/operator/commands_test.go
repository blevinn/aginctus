package operatorcmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestOperatorRegisterRejectsBeforeCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		options config.Options
	}{
		{"missing preflight project", []string{"operator", "preflight"}, config.Options{}},
		{"missing plan project", []string{"operator", "project-plan"}, config.Options{}},
		{"missing confirmation", []string{"operator", "project-ensure", "--project", "tenant"}, config.Options{}},
		{"unexpected args", []string{"operator", "units-audit", "extra"}, config.Options{}},
		{"offline config override", []string{"operator", "project-plan", "--project", "tenant"}, config.Options{ConfigurationFile: "/etc/untrusted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tc.args)
			status, loads, handlers := 0, 0, 0
			unit := func(context.Context, []string, io.Writer, io.Writer) int { handlers++; return 0 }
			Register(root, &output, &output, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, &tc.options, Handlers{
				UnitsAudit:    unit,
				Preflight:     unit,
				ProjectPlan:   unit,
				ProjectEnsure: unit,
			})
			if err := root.Execute(); err == nil {
				t.Fatal("accepted invalid operator input")
			}
			if loads != 0 || handlers != 0 {
				t.Fatalf("loads=%d handlers=%d", loads, handlers)
			}
		})
	}
}

func TestOperatorRegisterDispatchesExplicitProject(t *testing.T) {
	for _, tc := range []struct {
		name           string
		args, expected []string
		loads          int
	}{
		{"plan", []string{"operator", "project-plan", "--project", "tenant"}, []string{"--project", "tenant"}, 0},
		{"ensure", []string{"operator", "project-ensure", "--project", "tenant", "--confirm-create"}, []string{"--project", "tenant", "--confirm-create"}, 0},
		{"preflight", []string{"operator", "preflight", "--project", "tenant"}, []string{"preflight", "--project", "tenant"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tc.args)
			status, loads, calls := 0, 0, 0
			options := config.Options{}
			record := func(_ context.Context, args []string, _, _ io.Writer) int {
				calls++
				if !reflect.DeepEqual(args, tc.expected) {
					t.Fatalf("args = %q; want %q", args, tc.expected)
				}
				return 0
			}
			Register(root, &output, &output, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, &options, Handlers{
				ProjectPlan:   record,
				ProjectEnsure: record,
				Preflight:     record,
			})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if loads != tc.loads || calls != 1 {
				t.Fatalf("loads=%d calls=%d", loads, calls)
			}
		})
	}
}

func TestOperatorRegisterHelpDoesNotLoadOrMutate(t *testing.T) {
	var output bytes.Buffer
	root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"operator", "project-ensure", "--help"})
	options := config.Options{}
	loads, calls, status := 0, 0, 0
	Register(root, &output, &output, func() (*config.Config, error) {
		loads++
		return &config.Config{}, nil
	}, &status, &options, Handlers{
		ProjectEnsure: func(context.Context, []string, io.Writer, io.Writer) int { calls++; return 0 },
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if loads != 0 || calls != 0 {
		t.Fatalf("help triggered loads=%d calls=%d", loads, calls)
	}
}
func TestStateAuditRequiresLocationAndDispatches(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{name: "missing location", args: []string{"operator", "state-audit"}, wantErr: true},
		{name: "old owner flag", args: []string{"operator", "state-audit", "--owner", "user"}, wantErr: true},
		{name: "user location", args: []string{"operator", "state-audit", "--location", "user"}, want: "user"},
		{name: "system location", args: []string{"operator", "state-audit", "--location=system"}, want: "system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(tc.args)
			loads, calls, status := 0, 0, 0
			var got []string
			Register(root, &stdout, &stderr, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, &config.Options{}, Handlers{
				StateAudit: func(args []string, _, _ io.Writer, _ *config.Config) int {
					calls++
					got = append([]string(nil), args...)
					return 17
				},
				UnitsAudit:    func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
				Preflight:     func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
				ProjectEnsure: func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
				ProjectPlan:   func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
			})
			err := root.Execute()
			if tc.wantErr {
				if err == nil {
					t.Fatal("invalid state-audit syntax was accepted")
				}
				if loads != 0 || calls != 0 || status != 0 {
					t.Fatalf("invalid syntax reached handler: loads=%d calls=%d status=%d", loads, calls, status)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if loads != 1 || calls != 1 || status != 17 {
				t.Fatalf("loads=%d calls=%d status=%d", loads, calls, status)
			}
			want := []string{"state-audit", "--location", tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("handler args=%q, want %q", got, want)
			}
		})
	}
}
