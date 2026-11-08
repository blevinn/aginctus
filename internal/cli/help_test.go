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
	if code := Run(context.Background(), []string{"--help"}, &top, &bytes.Buffer{}, nil, nil); code != 0 || top.Len() == 0 {
		t.Fatalf("root help: code=%d output=%q", code, top.String())
	}
	for _, family := range []string{"apply", "gateway", "network", "herdr", "workload", "images", "config", "doctor", "operator", "status", "remove", "version", "testenv"} {
		for _, args := range [][]string{{"help", family}, {family, "--help"}, {family, "-h"}, {"--configuration-file=/nonexistent", family, "--help"}} {
			var out, errs bytes.Buffer
			if code := Run(context.Background(), args, &out, &errs, nil, nil); code != 0 || out.Len() == 0 || errs.Len() != 0 {
				t.Fatalf("%q: code=%d stdout=%q stderr=%q", args, code, out.String(), errs.String())
			}
		}
	}
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs, nil, nil); code != 0 || out.String() != top.String() || errs.Len() != 0 {
			t.Fatalf("%q: code=%d stdout=%q stderr=%q", args, code, out.String(), errs.String())
		}
	}
	for _, args := range [][]string{{"help", "phantom"}, {"phantom", "--help"}, {"help", "gateway", "extra"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs, nil, nil); code != 2 || out.Len() != 0 || errs.Len() == 0 {
			t.Fatalf("%q: code=%d stdout=%q stderr=%q", args, code, out.String(), errs.String())
		}
	}
	for _, word := range []string{"gateway", "network", "herdr", "workload", "images", "operator", "testenv", "--configuration-file"} {
		if !strings.Contains(top.String(), word) {
			t.Errorf("root help missing %q", word)
		}
	}
}

func TestHelpGeneratedFamilyTreeMatchesCanonicalSyntax(t *testing.T) {
	for _, tc := range []struct {
		family string
		routes []string
	}{
		{"gateway", []string{"aginctus gateway provider register TYPE", "aginctus gateway provider show ALIAS", "aginctus gateway model register NAME", "aginctus gateway model render NAME", "aginctus gateway egress firewall plan", "aginctus gateway egress dhcp apply", "aginctus gateway responses verify"}},
		{"network", []string{"aginctus network apply", "aginctus network firewall egress reconcile"}},
		{"herdr", []string{"aginctus herdr authorize WORKLOAD", "aginctus herdr client status"}},
		{"workload", []string{"aginctus workload apply [NAME]", "aginctus workload start [NAME]", "aginctus workload gateway provision WORKLOAD"}},
		{"images", []string{"aginctus images import ALIAS PATH", "aginctus images owner-audit ALIAS", "aginctus images import-owned ALIAS PATH", "aginctus images replace-plan ALIAS PATH"}},
		{"operator", []string{"aginctus operator project-ensure", "aginctus operator units-audit"}},
		{"testenv", []string{"aginctus testenv vm plan SCENARIO", "aginctus testenv vm test SCENARIO"}},
	} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), []string{"help", tc.family}, &out, &errs, nil, nil); code != 0 || errs.Len() != 0 {
			t.Fatalf("%s: code=%d stderr=%q", tc.family, code, errs.String())
		}
		for _, route := range tc.routes {
			if !strings.Contains(out.String(), route) {
				t.Errorf("%s help missing %q: %s", tc.family, route, out.String())
			}
		}
		var direct bytes.Buffer
		if code := Run(context.Background(), []string{tc.family, "--help"}, &direct, &bytes.Buffer{}, nil, nil); code != 0 || direct.String() != out.String() {
			t.Errorf("%s family help differs from help command", tc.family)
		}
	}
}

func TestHelpLeafFlagsAndSafetyClaims(t *testing.T) {
	for _, tc := range []struct {
		args []string
		required []string
	}{
		{[]string{"help", "gateway"}, []string{"--confirm-stopped-instance", "--confirm-bridge-only", "--uplink-zone", "does not activate inference", "preserves persistent volumes"}},
		{[]string{"help", "workload"}, []string{"--project", "--confirm-start", "--confirm-stop", "compatibility mapping"}},
		{[]string{"help", "images"}, []string{"--project", "--owner", "--confirm-create"}},
		{[]string{"help", "apply"}, []string{"--from", "--render"}},
		{[]string{"gateway", "model", "register", "--help"}, []string{"--provider", "--source", "--dry-run"}},
	} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), tc.args, &out, &errs, nil, nil); code != 0 || errs.Len() != 0 {
			t.Fatalf("%q: code=%d stderr=%q", tc.args, code, errs.String())
		}
		for _, word := range tc.required {
			if !strings.Contains(out.String(), word) {
				t.Errorf("%q help missing %q", tc.args, word)
			}
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
