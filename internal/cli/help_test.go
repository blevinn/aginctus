package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/workload"
)

func TestHelpOfflineInventory(t *testing.T) {
	var top bytes.Buffer
	printUsage(&top)
	for _, command := range []string{"apply", "gateway", "network", "herdr", "workload", "images", "config", "doctor", "status", "remove", "version", "help"} {
		for _, args := range [][]string{{"help", command}, {command, "--help"}, {command, "-h"}, {"--configuration-file=/nonexistent", command, "--help"}} {
			var out, errOut bytes.Buffer
			// Nil dependencies prove help cannot load config or contact Incus.
			if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 0 || out.Len() == 0 || errOut.Len() != 0 {
				t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
			}
		}
	}
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var out bytes.Buffer
		if code := Run(context.Background(), args, &out, &out, nil, nil); code != 0 || out.String() != top.String() {
			t.Fatalf("%v: code=%d help=%q", args, code, out.String())
		}
	}
	for _, args := range [][]string{{"help", "phantom"}, {"phantom", "--help"}, {"help", "gateway", "extra"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
	for _, printFamily := range []func(*bytes.Buffer){
		func(w *bytes.Buffer) { printGatewayUsage(w) },
		func(w *bytes.Buffer) { printNetworkUsage(w) },
		func(w *bytes.Buffer) { printHerdrClientUsage(w) },
		func(w *bytes.Buffer) { printWorkloadUsage(w) },
		func(w *bytes.Buffer) { printApplyUsage(w) },
	} {
		var family bytes.Buffer
		printFamily(&family)
		if !strings.Contains(top.String(), family.String()) {
			t.Fatalf("top-level inventory differs from family usage: %s", family.String())
		}
	}
	for _, want := range []string{
		"YAML/JSON multi-document", "--from=FILE", "not stdin or a symlink", "16 MiB",
		"gateway egress preflight", "gateway egress audit",
		"gateway egress dhcp plan --ranges=START-END",
		"gateway egress dhcp apply --ranges=START-END --confirm-bridge-only",
		"gateway egress reconcile --dry-run", "gateway egress reconcile --confirm-stopped-instance",
		"network firewall policy", "network firewall reconcile", "herdr authorize", "herdr pin-host", "herdr register",
		"workload gateway provision", "--require-function-call", "network ensure", "herdr client ensure", "workload <name> ensure|teardown",
	} {
		if !strings.Contains(top.String(), want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestHelpGatewayRegistrationSyntax(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"help", "gateway"}, {"gateway", "--help"}, {"gateway", "-h"}, {"--configuration-file=/nonexistent", "gateway", "--help"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 0 || errOut.Len() != 0 {
			t.Fatalf("%v: code=%d stderr=%q", args, code, errOut.String())
		}
		for _, syntax := range []string{
			"gateway provider chatgpt register [--alias=NAME] [--dry-run]",
			"gateway provider opencode-go register [--alias=NAME] [--dry-run]",
			"gateway model NAME register [--provider=ALIAS] [--source=chatgpt/MODEL|openai/MODEL] [--dry-run]",
		} {
			if count := strings.Count(out.String(), syntax); count != 1 {
				t.Errorf("%v: expected one occurrence of %q, got %d", args, syntax, count)
			}
		}
		if strings.Contains(out.String(), "[--source=chatgpt/MODEL]") {
			t.Errorf("%v: stale ChatGPT-only model grammar", args)
		}
	}
}

func TestHelpAdvertisedAliasesDispatch(t *testing.T) {
	stubManagementNetwork(t, func(_ context.Context, _ managementnetwork.Spec, o managementnetwork.Options) error {
		if !o.DryRun {
			t.Fatal("expected dry-run")
		}
		return nil
	})
	stubHerdrClient(t, func(_ context.Context, _ herdrclient.Spec, o herdrclient.Options) error {
		if !o.DryRun {
			t.Fatal("expected dry-run")
		}
		return nil
	})
	stubWorkload(t, func(_ context.Context, _ workload.Spec, o workload.Options) error {
		if !o.DryRun {
			t.Fatal("expected dry-run")
		}
		return nil
	})
	for _, pair := range [][2]string{
		{"network apply", "network ensure"}, {"network remove", "network teardown"},
		{"herdr client apply", "herdr client ensure"}, {"herdr client remove", "herdr client teardown"},
		{"workload apply dev", "workload dev ensure"}, {"workload remove dev", "workload dev teardown"},
	} {
		var first string
		for i, command := range pair {
			var out, errOut bytes.Buffer
			args := append(strings.Fields(command), "--dry-run")
			if code := Run(context.Background(), args, &out, &errOut, nil, &fakeConfigLoader{}); code != 0 {
				t.Fatalf("%v: code=%d stderr=%s", args, code, errOut.String())
			}
			if i == 0 {
				first = out.String()
			} else if first != out.String() {
				t.Fatalf("alias differs: %v", pair)
			}
		}
	}
}

func TestHelpEgressPreviewAndConfirmationSyntax(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	cfg, err := loader.Load(config.Options{Overrides: []string{"gateway.egress.ipv4.address=10.20.30.25/24"}})
	if err != nil {
		t.Fatal(err)
	}
	previous := gatewayEgressQuery
	t.Cleanup(func() { gatewayEgressQuery = previous })
	called := false
	gatewayEgressQuery = func(context.Context, ...string) ([]byte, error) {
		called = true
		return nil, errors.New("hermetic inspection stop")
	}
	for _, command := range []string{"egress audit", "egress dhcp plan --ranges=10.20.30.100-10.20.30.200", "egress reconcile --dry-run"} {
		called = false
		var out, errOut bytes.Buffer
		code := Run(context.Background(), append([]string{"gateway"}, strings.Fields(command)...), &out, &errOut, nil, &fakeConfigLoader{cfg: cfg})
		if code != 1 || !called || !strings.Contains(errOut.String(), "hermetic inspection stop") {
			t.Fatalf("%s: code=%d inspected=%v stderr=%s", command, code, called, errOut.String())
		}
	}
	for _, command := range []string{"egress reconcile", "egress reconcile --force", "egress dhcp apply --ranges=10.20.30.100-10.20.30.200", "apply --dry-run"} {
		called = false
		var out, errOut bytes.Buffer
		code := Run(context.Background(), append([]string{"gateway"}, strings.Fields(command)...), &out, &errOut, nil, &fakeConfigLoader{})
		if code != 2 || called || !strings.Contains(errOut.String(), "Usage:") {
			t.Fatalf("%s: code=%d inspected=%v stderr=%s", command, code, called, errOut.String())
		}
	}
}

func TestHelpApplyFromStreamSyntax(t *testing.T) {
	previous := executeAggregatePlan
	t.Cleanup(func() { executeAggregatePlan = previous })
	var executed orchestration.Plan
	executeAggregatePlan = func(_ context.Context, p orchestration.Plan, _ *config.Config) error { executed = p; return nil }
	jsonDoc := func(id string) string {
		return fmt.Sprintf(`{"apiVersion":"aginctus.dev/v1alpha1","kind":"Orchestration","metadata":{"name":"test"},"steps":[{"id":%q,"driver":"apply","configuration":{}}]}`, id)
	}
	yamlDoc := func(id string) string {
		return fmt.Sprintf("apiVersion: aginctus.dev/v1alpha1\nkind: Orchestration\nmetadata: {name: test}\nsteps:\n- id: %s\n  driver: apply\n  configuration: {}\n", id)
	}
	for _, stream := range []string{jsonDoc("first") + "\n---\n" + jsonDoc("second"), yamlDoc("first") + "---\n" + yamlDoc("second"), yamlDoc("first") + "---\n" + jsonDoc("second")} {
		path := filepath.Join(t.TempDir(), "stream")
		if err := os.WriteFile(path, []byte(stream), 0600); err != nil {
			t.Fatal(err)
		}
		for _, from := range [][]string{{"--from", path}, {"--from=" + path}} {
			var out, errOut bytes.Buffer
			executed = orchestration.Plan{}
			code := Run(context.Background(), append([]string{"apply"}, from...), &out, &errOut, nil, &fakeConfigLoader{})
			if code != 0 || len(executed.Steps) != 2 || executed.Steps[0].ID != "first" || executed.Steps[1].ID != "second" {
				t.Fatalf("%v: code=%d plan=%+v stderr=%s", from, code, executed, errOut.String())
			}
		}
	}
}
