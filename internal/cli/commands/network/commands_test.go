package networkcmd

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

// Network syntax is owned by its Register implementation; invalid input
// must not load configuration or call host/Incus capabilities.
func TestNetworkRegistrationRejectsMalformedBeforeCapabilities(t *testing.T) {
	for _, args := range [][]string{
		{"network", "ensure", "--wat"},
		{"network", "apply", "--dry-run", "extra"},
		{"network", "status", "--force"},
		{"network", "firewall", "policy", "--force"},
		{"network", "firewall", "egress", "revoke", "oops"},
		{"network", "firewall", "reconcile", "--force"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errs bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&out)
			root.SetErr(&errs)
			root.SetArgs(args)
			loaded, called, status := 0, 0, 0
			Register(root, &out, &errs, func() (*config.Config, error) {
				loaded++
				return &config.Config{}, nil
			}, &status, Handlers{
				Operation: func(context.Context, string, bool, bool, io.Writer, io.Writer, *config.Config) int { called++; return 0 },
				Status: func(context.Context, io.Writer, io.Writer, *config.Config) int { called++; return 0 },
				FirewallPolicy: func(context.Context, io.Writer, io.Writer, *config.Config) int { called++; return 0 },
				FirewallReconcile: func(context.Context, io.Writer, io.Writer, *config.Config) int { called++; return 0 },
				FirewallEgress: func(context.Context, io.Writer, io.Writer, *config.Config, bool) int { called++; return 0 },
			})
			if err := root.Execute(); err == nil {
				t.Fatal("accepted invalid network syntax")
			}
			if loaded != 0 || called != 0 || status != 0 || out.Len() != 0 {
				t.Fatalf("invalid network syntax contacted capabilities: load=%d handlers=%d status=%d out=%q", loaded, called, status, out.String())
			}
		})
	}
}

func TestNetworkRegistrationDispatchesToExactCapability(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"dry run apply", []string{"network", "apply", "--dry-run"}, "operation:upsert:true:false"},
		{"legacy ensure", []string{"network", "ensure", "--force"}, "operation:upsert:false:true"},
		{"remove", []string{"network", "remove", "--force"}, "operation:delete:false:true"},
		{"read status", []string{"network", "status"}, "status"},
		{"firewall policy", []string{"network", "firewall", "policy"}, "firewall-policy"},
		{"firewall reconcile", []string{"network", "firewall", "reconcile"}, "firewall-reconcile"},
		{"egress reconcile", []string{"network", "firewall", "egress", "reconcile"}, "firewall-egress:false"},
		{"egress revoke", []string{"network", "firewall", "egress", "revoke"}, "firewall-egress:true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceUsage: true, SilenceErrors: true}
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(tc.args)
			loads, calls, status := 0, 0, 0
			got := ""
			record := func(label string) int { calls++; got = label; return 19 }
			Register(root, &stdout, &stderr, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, Handlers{
				Operation: func(_ context.Context, op string, dry, force bool, _, _ io.Writer, _ *config.Config) int {
					return record(fmt.Sprintf("operation:%s:%t:%t", op, dry, force))
				},
				Status: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record("status") },
				FirewallPolicy: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record("firewall-policy") },
				FirewallReconcile: func(context.Context, io.Writer, io.Writer, *config.Config) int { return record("firewall-reconcile") },
				FirewallEgress: func(_ context.Context, _, _ io.Writer, _ *config.Config, revoke bool) int {
					return record(fmt.Sprintf("firewall-egress:%t", revoke))
				},
			})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if status != 19 || calls != 1 || loads != 1 || got != tc.want {
				t.Fatalf("status=%d calls=%d loads=%d action=%q, want %q", status, calls, loads, got, tc.want)
			}
		})
	}
}
