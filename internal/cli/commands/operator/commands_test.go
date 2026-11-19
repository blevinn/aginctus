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
				UnitsAudit: func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
				Preflight: func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
				ProjectEnsure: func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
				ProjectPlan: func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
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
