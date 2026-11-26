package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/spf13/cobra"
)

type planPublished struct {
	rev       *environment.AcceptedRevision
	candidate *environment.Candidate
}

func (p planPublished) ReadAccepted(context.Context, environment.Descriptor) (*environment.AcceptedRevision, *environment.Candidate, error) {
	return p.rev, p.candidate, nil
}

func TestNativeIntentPlanProjectsAuthenticatedSnapshot(t *testing.T) {
	desc := environment.Descriptor{ID: phaseOneEnvironmentID, Name: "dev", Binding: environment.Binding{ServerID: "test-server", Project: "team"}}
	workload := func(id, bridge string) acceptedstate.WorkloadSpec {
		return acceptedstate.WorkloadSpec{ID: id, InstanceName: "vm-" + id, InstanceType: "container", ImageFingerprint: strings.Repeat("a", 64), Profiles: []string{},
			Native: &acceptedstate.WorkloadNativeSpec{Runtime: "fixture-runtime", StoragePool: "fixture-pool", ManagementNetwork: bridge}}
	}
	c := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: desc,
		SelectedWorkloads: []string{"beta", "alpha", "other", "legacy"}, Workloads: []acceptedstate.WorkloadSpec{
			workload("beta", "bridge"), workload("alpha", "bridge"), workload("other", "other-bridge"), workload("legacy", "bridge"),
		}}
	c.Workloads[3].Native = nil
	rev, err := environment.BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	model := environment.AcceptedReadModel{Catalog: environment.Catalog{
		Verifier: phaseOneVerifier{binding: desc.Binding}, Source: phaseOneEnrollment{descriptor: desc},
	}, Source: planPublished{&rev, &c}}
	calls := 0
	read := func(ctx context.Context, project, id string) (environment.AcceptedRevision, environment.Candidate, error) {
		calls++
		return model.ReadAcceptedSnapshot(ctx, project, id)
	}
	run := func(project, id string, args ...string) (string, error) {
		var out bytes.Buffer
		state := &cobra.Command{Use: "state", SilenceErrors: true, SilenceUsage: true}
		registerNativeIntentPlan(state, &out, func() (clioutput.Format, error) { return clioutput.JSON, nil }, &project, &id, read)
		state.SetArgs(append([]string{"plan-native"}, args...))
		err := state.ExecuteContext(t.Context())
		return out.String(), err
	}
	out, err := run("team", desc.ID, "--workload", "beta", "--workload", "alpha")
	if err != nil || calls != 1 {
		t.Fatalf("expected a single authenticated snapshot read: %v; calls=%d", err, calls)
	}
	second, err := run("team", desc.ID, "--workload", "alpha", "--workload", "beta")
	if err != nil || out != second || calls != 2 {
		t.Fatalf("argument order changed the preview: %v", err)
	}
	var preview orchestration.AcceptedIntentPreview
	if err := json.Unmarshal([]byte(out), &preview); err != nil || preview.Kind != "AcceptedIntentPreview" || preview.Mode != "intent-only" || preview.LiveState != "unobserved" || preview.WriterPin || preview.Selection.Revision != rev {
		t.Fatalf("invalid intent preview: %v\n%s", err, out)
	}
	var order []string
	for _, r := range preview.Selection.Resources {
		order = append(order, r.ID)
		if r.Kind == orchestration.NativeWorkload && !slices.Equal(r.DependsOn, []string{"management-network"}) {
			t.Fatalf("wrong workload prerequisites: %v", r.DependsOn)
		}
	}
	if !slices.Equal(order, []string{"management-network", "alpha", "beta"}) || strings.Contains(out, "fixture-pool") || strings.Contains(out, "fixture-runtime") {
		t.Fatalf("preview included extra state: %s", out)
	}
	for _, tc := range []struct {
		project, id string
		args        []string
		reads       bool
	}{
		{"team", desc.ID, nil, false},
		{"team", desc.ID, []string{"--all", "--herdr"}, false},
		{"team", desc.ID, []string{"--all"}, true}, // Must not silently drop unsupported accepted workloads.
		{"team", desc.ID, []string{"--herdr"}, true},
		{"team", desc.ID, []string{"--gateway"}, true},
		{"team", desc.ID, []string{"--workload", "alpha", "--workload", "alpha"}, false},
		{"team", "dev", []string{"--workload", "alpha"}, false},
		{"foreign", desc.ID, []string{"--workload", "alpha"}, true},
		{"team", desc.ID, []string{"--workload", "unselected"}, true},
		{"team", desc.ID, []string{"--workload", "legacy"}, true},
		{"team", desc.ID, []string{"--workload", "alpha", "--workload", "other"}, true},
	} {
		before := calls
		if output, err := run(tc.project, tc.id, tc.args...); err == nil || output != "" || (calls > before) != tc.reads {
			t.Fatalf("invalid selection did not fail closed: %+v; err=%v; out=%q", tc, err, output)
		}
	}
	model.Source = phaseOnePublished{}
	if output, err := run("team", desc.ID, "--workload", "alpha"); err == nil || output != "" {
		t.Fatal("verified absence produced a plan")
	}
	c.Workloads[0].Native = nil // rev now disagrees with the published body.
	model.Source = planPublished{&rev, &c}
	if output, err := run("team", desc.ID, "--workload", "alpha"); err == nil || output != "" {
		t.Fatal("tampered accepted snapshot produced a plan")
	}
	c.SelectedWorkloads = []string{"beta", "alpha"}
	c.Workloads = []acceptedstate.WorkloadSpec{workload("beta", "bridge"), workload("alpha", "bridge")}
	c.Herdr = &environment.HerdrSpec{InstanceName: "test-herdr", ImageFingerprint: strings.Repeat("b", 64), StoragePool: "fixture-pool", ManagementNetwork: "bridge", AllowStateBootstrap: true}
	image := func(alias, digit string) environment.GatewayImageSpec {
		return environment.GatewayImageSpec{Registry: "registry.example", Repository: alias, Architecture: "amd64", Alias: alias,
			IncusFingerprint: strings.Repeat(digit, 64), ManifestDigest: "sha256:" + strings.Repeat(digit, 64), ConfigDigest: "sha256:" + strings.Repeat("d", 64)}
	}
	c.Gateway = &environment.GatewaySpec{ManagementNetwork: "gateway-bridge", RootPool: "fixture-pool", DatabasePool: "fixture-pool",
		Postgres: image("postgres", "e"), LiteLLM: image("litellm", "f"), EnsureRunning: true, CredentialVersion: "fixture-version"}
	rev, err = environment.BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	full, err := run("team", desc.ID, "--all")
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := run("team", desc.ID, "--gateway", "--workload", "alpha", "--herdr", "--workload", "beta")
	if err != nil || mixed != full {
		t.Fatalf("full and equivalent explicit selection disagree: %v", err)
	}
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--all"}, []string{"gateway-image:", "gateway-volume:gateway-image", "gateway-instance:gateway-volume", "management-network:", "alpha:management-network", "beta:management-network", "herdr-system-state:management-network", "herdr-client:herdr-system-state"}},
		{[]string{"--gateway"}, []string{"gateway-image:", "gateway-volume:gateway-image", "gateway-instance:gateway-volume"}},
		{[]string{"--herdr"}, []string{"management-network:", "herdr-system-state:management-network", "herdr-client:herdr-system-state"}},
	} {
		output, err := run("team", desc.ID, tc.args...)
		if err != nil || json.Unmarshal([]byte(output), &preview) != nil || preview.WriterPin || preview.LiveState != "unobserved" {
			t.Fatalf("invalid scope preview: %v", err)
		}
		var got []string
		for _, r := range preview.Selection.Resources {
			got = append(got, r.ID+":"+strings.Join(r.DependsOn, ","))
		}
		if !slices.Equal(got, tc.want) || strings.Contains(output, "fixture-version") {
			t.Fatalf("incorrect dependency graph or leaked intent: %v", got)
		}
	}
	if err := json.Unmarshal([]byte(full), &preview); err != nil {
		t.Fatal(err)
	}
	wantCapabilities := []orchestration.AcceptedIntentCapability{
		{ID: "execution-authority", Status: "required-unverified"}, {ID: "host-attachments", Status: "required-unverified"},
		{ID: "herdr-state-bootstrap", Status: "separate-approval-required"}, {ID: "gateway-image-provenance", Status: "required-unverified"},
		{ID: "gateway-runtime", Status: "gated"},
	}
	if !slices.Equal(preview.Capabilities, wantCapabilities) {
		t.Fatalf("full preview did not preserve capability gates: %+v", preview.Capabilities)
	}
}

func TestInstalledNativeIntentPlanRequiresExplicitReadConfig(t *testing.T) {
	t.Setenv("AGINCTUS_RESTRICTED_READ_CONFIG", "")
	var out, stderr bytes.Buffer
	args := []string{"--incus-project", "team", "--environment", phaseOneEnvironmentID, "state", "plan-native", "--workload", "alpha", "-o", "table"}
	if code := Run(t.Context(), args, &out, &stderr, nil, nil); code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "AGINCTUS_RESTRICTED_READ_CONFIG") {
		t.Fatalf("installed invocation failed open or is not registered: code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}
