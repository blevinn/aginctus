package compose

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration"
	composeclient "github.com/lxc/incus-compose/client"
	"github.com/lxc/incus-compose/iclient"
	"github.com/lxc/incus/v7/shared/api"
)

func TestValidateAcceptsComposeConfiguration(t *testing.T) {
	driver := New()
	err := driver.Validate(json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {
			"services": {
				"web": {
					"image": "alpine:latest"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnknownConfigurationField(t *testing.T) {
	driver := New()
	err := driver.Validate(json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {"services": {}},
		"unexpected": true
	}`))
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestSupportsDryRunIsFalse(t *testing.T) {
	if New().SupportsDryRun() {
		t.Fatal("SupportsDryRun() = true, want false until incus-compose exposes planning")
	}
}

func TestExecuteUsesValidatedConfiguration(t *testing.T) {
	driver := New()
	var got Config
	driver.execute = func(_ context.Context, cfg Config, _ map[string]string) error {
		got = cfg
		return nil
	}

	err := driver.Execute(context.Background(), json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {
			"services": {
				"web": {
					"image": "alpine:latest"
				}
			}
		}
	}`), orchestration.ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Project != "aginctus-test" {
		t.Fatalf("project = %q, want aginctus-test", got.Project)
	}
}

func TestExecuteRejectsDryRun(t *testing.T) {
	driver := New()
	err := driver.Execute(context.Background(), json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {"services": {}}
	}`), orchestration.ExecuteOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "does not support dry-run") {
		t.Fatalf("Execute() error = %v, want dry-run capability error", err)
	}
}

func TestComposeServiceOrderAndFailFastAreSequential(t *testing.T) {
	visited := []string{}
	errFailure := errors.New("postgres failed")
	err := runComposeServicesSequentially(context.Background(), []string{"postgres", "litellm"}, func(service string) error {
		visited = append(visited, service)
		if service == "postgres" {
			return errFailure
		}
		return nil
	})
	if !errors.Is(err, errFailure) {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(visited, []string{"postgres"}) {
		t.Fatalf("visited = %v", visited)
	}
}

func TestComposeServiceExecutionWaitsForActiveResource(t *testing.T) {
	release := make(chan struct{})
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		done <- runComposeServicesSequentially(context.Background(), []string{"postgres", "litellm"}, func(service string) error {
			if service == "postgres" {
				close(started)
				<-release
				return errors.New("failed after active work")
			}
			t.Error("dependent started after prerequisite failed")
			return nil
		})
	}()
	<-started
	select {
	case <-done:
		t.Fatal("returned while postgres resource remained active")
	default:
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("expected failure")
	}
}

func TestComposeServiceExecutionStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var visited []string
	err := runComposeServicesSequentially(ctx, []string{"postgres", "litellm"}, func(s string) error {
		visited = append(visited, s)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(visited, []string{"postgres"}) {
		t.Fatalf("visited = %v", visited)
	}
}

func TestComposeClientConstructionProvidesLocalConnection(t *testing.T) {
	var called bool
	client, err := newComposeGlobalClient(context.Background(), func(info *iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
		called = true
		if info.Name != "local" || len(info.Addrs) != 1 || info.Addrs[0] != "unix://" {
			t.Fatalf("unexpected connection info: %#v", info)
		}
		return iclient.NewConnection(info)
	})
	if err != nil || !called || client == nil {
		t.Fatalf("newComposeGlobalClient() = %v, %v, called=%t", client, err, called)
	}
}

func TestComposeClientConstructionRejectsConnectionError(t *testing.T) {
	want := errors.New("unavailable socket")
	client, err := newComposeGlobalClient(context.Background(), func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
		return nil, want
	})
	if client != nil || !errors.Is(err, want) {
		t.Fatalf("newComposeGlobalClient() = %v, %v", client, err)
	}
}

func TestExplainComposeConnectionError(t *testing.T) {
	err := errors.New("the incus server is not listening on the network (core.https_address is not set)")
	got := explainComposeConnectionError(err)
	if !errors.Is(got, err) || !strings.Contains(got.Error(), "incus config get core.https_address") {
		t.Fatalf("missing actionable connection diagnostic: %v", got)
	}
	other := errors.New("unrelated connection failure")
	if got := explainComposeConnectionError(other); got != other {
		t.Fatalf("unrelated error altered: %v", got)
	}
}

func TestComposePrerequisitesPrecedeInstances(t *testing.T) {
	for _, priority := range []int{
		composeclient.PriorityProject,
		composeclient.PriorityProfile,
		composeclient.PriorityImage,
		composeclient.PriorityNetwork,
		composeclient.PriorityVolume,
	} {
		if !isComposePrerequisite(priority) {
			t.Fatalf("priority %d should be a prerequisite", priority)
		}
	}
	if isComposePrerequisite(composeclient.PriorityInstance) {
		t.Fatal("instances must remain in dependency-ordered service pass")
	}
}

func TestComposeProjectOwnershipValidation(t *testing.T) {
	project := "gateway-test"
	valid := map[string]string{
		ownershipManagedKey:    "true",
		ownershipResourceKey:   "compose-project",
		ownershipDeploymentKey: project,
	}
	if err := verifyComposeProjectOwnership(valid, project); err != nil {
		t.Fatalf("owned project rejected: %v", err)
	}
	for name, changes := range map[string]map[string]string{
		"unmarked":             {},
		"wrong deployment":     {ownershipManagedKey: "true", ownershipResourceKey: "compose-project", ownershipDeploymentKey: "other"},
		"wrong kind":           {ownershipManagedKey: "true", ownershipResourceKey: "compose-instance", ownershipDeploymentKey: project},
		"wrong managed marker": {ownershipManagedKey: "false", ownershipResourceKey: "compose-project", ownershipDeploymentKey: project},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyComposeProjectOwnership(changes, project); err == nil {
				t.Fatal("accepted foreign project")
			}
		})
	}
}

func TestComposeInstanceOwnershipValidation(t *testing.T) {
	const project = "gateway-test"
	valid := map[string]string{
		ownershipManagedKey:    "true",
		ownershipResourceKey:   "compose-instance",
		ownershipDeploymentKey: project,
	}
	if err := verifyComposeInstanceOwnership(valid, project, "postgres-1"); err != nil {
		t.Fatalf("owned instance rejected: %v", err)
	}
	cases := []struct {
		name   string
		config map[string]string
	}{
		{name: "unmarked", config: map[string]string{}},
		{name: "wrong deployment", config: map[string]string{ownershipManagedKey: "true", ownershipResourceKey: "compose-instance", ownershipDeploymentKey: "other"}},
		{name: "wrong kind", config: map[string]string{ownershipManagedKey: "true", ownershipResourceKey: "compose-project", ownershipDeploymentKey: project}},
		{name: "missing deployment", config: map[string]string{ownershipManagedKey: "true", ownershipResourceKey: "compose-instance"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyComposeInstanceOwnership(tc.config, project, "postgres-1"); err == nil {
				t.Fatal("accepted foreign instance")
			}
		})
	}
}

func TestDetectComposeInstanceDriftRejectsUndeclaredLiteLLMConfigFile(t *testing.T) {
	const key = "environment.CONFIG_FILE_PATH"
	desired := map[string]string{"user.label.incus-compose.service": "litellm"}
	actual := map[string]string{
		"user.label.incus-compose.service": "litellm",
		key: "/app/aginctus-go-pilot.yaml",
	}
	if err := detectComposeInstanceDrift(desired, nil, actual, nil); err == nil {
		t.Fatal("undocumented LiteLLM config override was silently accepted")
	}
	desired[key] = "/app/aginctus-go-pilot.yaml"
	if err := detectComposeInstanceDrift(desired, nil, actual, nil); err != nil {
		t.Fatalf("matching declarative LiteLLM override rejected: %v", err)
	}
	delete(actual, key)
	if err := detectComposeInstanceDrift(desired, nil, actual, nil); err != nil {
		t.Fatalf("newly declared config is safe to add: %v", err)
	}
	delete(desired, key)
	if err := detectComposeInstanceDrift(desired, nil, actual, nil); err != nil {
		t.Fatalf("fully cleared config still reported drift: %v", err)
	}
}

func TestDetectComposeInstanceDrift(t *testing.T) {
	tests := []struct {
		name           string
		desired        map[string]string
		actual         map[string]string
		profiles       []string
		actualProfiles []string
		wantDrift      bool
	}{
		{name: "unchanged", desired: map[string]string{"user.setting": "new"}, actual: map[string]string{"user.setting": "new"}},
		{name: "missing key is additive", desired: map[string]string{"user.setting": "new"}, actual: map[string]string{}},
		{name: "changed environment extension", desired: map[string]string{"environment.TEST": "new"}, actual: map[string]string{"environment.TEST": "old"}, wantDrift: true},
		{name: "changed command extension", desired: map[string]string{"oci.entrypoint": "new"}, actual: map[string]string{"oci.entrypoint": "old"}, wantDrift: true},
		{name: "changed image alias extension", desired: map[string]string{"user.image_alias": "new"}, actual: map[string]string{"user.image_alias": "old"}, wantDrift: true},
		{name: "changed profile", profiles: []string{"default", "new"}, actualProfiles: []string{"default", "old"}, wantDrift: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := detectComposeInstanceDrift(tc.desired, tc.profiles, tc.actual, tc.actualProfiles)
			if (err != nil) != tc.wantDrift {
				t.Fatalf("drift=%v, want %t", err, tc.wantDrift)
			}
			if tc.wantDrift && strings.Contains(err.Error(), "old") {
				t.Fatalf("drift error leaked a previous value: %v", err)
			}
		})
	}
}

func TestDetectComposeDeviceDrift(t *testing.T) {
	cases := []struct {
		name      string
		desired   map[string]map[string]string
		actual    map[string]map[string]string
		wantDrift bool
	}{
		{name: "unchanged", desired: map[string]map[string]string{"data": {"type": "disk", "source": "data", "path": "/data"}}, actual: map[string]map[string]string{"data": {"type": "disk", "source": "data", "path": "/data"}}},
		{name: "missing device", desired: map[string]map[string]string{"data": {"type": "disk", "path": "/data"}}, actual: map[string]map[string]string{}, wantDrift: true},
		{name: "changed device source", desired: map[string]map[string]string{"data": {"source": "new"}}, actual: map[string]map[string]string{"data": {"source": "old"}}, wantDrift: true},
		{name: "changed device path", desired: map[string]map[string]string{"data": {"path": "/new"}}, actual: map[string]map[string]string{"data": {"path": "/old"}}, wantDrift: true},
		{name: "extra device ignored", desired: map[string]map[string]string{}, actual: map[string]map[string]string{"unmanaged": {"type": "disk"}}},
		{name: "extra settings ignored", desired: map[string]map[string]string{"data": {"path": "/data"}}, actual: map[string]map[string]string{"data": {"path": "/data", "source": "other"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := detectComposeDeviceDrift(tc.desired, tc.actual)
			if (err != nil) != tc.wantDrift {
				t.Fatalf("drift=%v, want %t", err, tc.wantDrift)
			}
			if tc.wantDrift && strings.Contains(err.Error(), "old") {
				t.Fatalf("error leaked old device value: %v", err)
			}
		})
	}
}

func TestDetectComposeImageDrift(t *testing.T) {
	cases := []struct {
		name, want, got string
		drift           bool
	}{
		{name: "same image", want: "docker.io/library/postgres:17-alpine", got: "docker.io/library/postgres:17-alpine"},
		{name: "changed tag", want: "docker.io/library/postgres:18-alpine", got: "docker.io/library/postgres:17-alpine", drift: true},
		{name: "changed repository", want: "docker.io/library/postgres:17-alpine", got: "docker.io/other/postgres:17-alpine", drift: true},
		{name: "unknown original image", want: "docker.io/library/postgres:17-alpine", drift: true},
		{name: "no image configured", got: "docker.io/library/postgres:17-alpine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := detectComposeImageDrift(tc.want, tc.got)
			if (err != nil) != tc.drift {
				t.Fatalf("detectComposeImageDrift(%q, %q) = %v, want drift %t", tc.want, tc.got, err, tc.drift)
			}
		})
	}
}

func TestDetectComposeImageFingerprintDrift(t *testing.T) {
	cases := []struct {
		name, desired, actual string
		wantError             bool
	}{
		{name: "same fingerprint", desired: "abcdef0123", actual: "abcdef0123"},
		{name: "case insensitive hex", desired: "ABCDEF0123", actual: "abcdef0123"},
		{name: "refreshed tag", desired: "abcdef0123", actual: "123456abcd", wantError: true},
		{name: "missing instance fingerprint", desired: "abcdef0123", wantError: true},
		{name: "missing alias fingerprint", actual: "abcdef0123", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := detectComposeImageFingerprintDrift(tc.desired, tc.actual)
			if (err != nil) != tc.wantError {
				t.Fatalf("got error %v; want error %t", err, tc.wantError)
			}
			if err != nil && (strings.Contains(err.Error(), tc.desired) && tc.desired != "" || strings.Contains(err.Error(), tc.actual) && tc.actual != "") {
				t.Fatalf("error contains fingerprint: %v", err)
			}
		})
	}
}

func TestVerifyComposeEnsuredImage(t *testing.T) {
	cases := []struct {
		name, alias, recordedAlias, currentFingerprint, baseFingerprint string
		wantErr                                                         bool
	}{
		{name: "unchanged after cache", alias: "docker.io/library/postgres:17-alpine", recordedAlias: "docker.io/library/postgres:17-alpine", currentFingerprint: "abc123", baseFingerprint: "abc123"},
		{name: "alias changed", alias: "docker.io/library/postgres:18-alpine", recordedAlias: "docker.io/library/postgres:17-alpine", currentFingerprint: "abc123", baseFingerprint: "abc123", wantErr: true},
		{name: "new cache differs", alias: "docker.io/library/postgres:17-alpine", recordedAlias: "docker.io/library/postgres:17-alpine", currentFingerprint: "def456", baseFingerprint: "abc123", wantErr: true},
		{name: "alias absent", alias: "docker.io/library/postgres:17-alpine", currentFingerprint: "abc123", baseFingerprint: "abc123", wantErr: true},
		{name: "fingerprint missing", alias: "docker.io/library/postgres:17-alpine", recordedAlias: "docker.io/library/postgres:17-alpine", baseFingerprint: "abc123", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyComposeEnsuredImage(tc.alias, tc.recordedAlias, tc.currentFingerprint, tc.baseFingerprint)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %v, want error %t", err, tc.wantErr)
			}
		})
	}
}

func TestComposeDriftErrorsAreTypedAndSecretSafe(t *testing.T) {
	const secret = "super-secret-value"
	cases := []struct {
		name string
		err  error
		kind ComposeDriftKind
	}{
		{"config", detectComposeInstanceDrift(map[string]string{"environment.TOKEN": secret}, nil, map[string]string{"environment.TOKEN": "previous-secret"}, nil), ComposeDriftConfig},
		{"profiles", detectComposeInstanceDrift(nil, []string{"new"}, nil, []string{"old"}), ComposeDriftProfiles},
		{"device", detectComposeDeviceDrift(map[string]map[string]string{"disk": {"source": secret}}, map[string]map[string]string{"disk": {"source": "old"}}), ComposeDriftDevice},
		{"image alias", detectComposeImageDrift("new-image", "old-image"), ComposeDriftImage},
		{"image fingerprint", detectComposeImageFingerprintDrift("abcdef", "123456"), ComposeDriftImage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var drift *ComposeDriftError
			if !errors.As(tc.err, &drift) || drift.Kind != tc.kind {
				t.Fatalf("error = %v, want typed %q drift", tc.err, tc.kind)
			}
			if strings.Contains(tc.err.Error(), secret) || strings.Contains(tc.err.Error(), "previous-secret") {
				t.Fatalf("drift error exposed secret: %v", tc.err)
			}
		})
	}
}

func TestComposeNeedsStartSkipsRunningInstance(t *testing.T) {
	// A freshly constructed instance has no fetched state and still needs
	// its initial start after Ensure. Live running-state behavior is covered
	// by TestIncusComposeReconciliationIntegration.
	if !composeNeedsStart(nil) {
		t.Fatal("non-instance resources must retain their Start action")
	}
}

func TestComposeDeviceAndProfileDriftRecreationDecisions(t *testing.T) {
	tests := []struct {
		name  string
		drift func() error
		kind  ComposeDriftKind
	}{
		{"profile changes", func() error {
			return detectComposeInstanceDrift(nil, []string{"new-profile"}, nil, []string{"old-profile"})
		}, ComposeDriftProfiles},
		{"raw device changes", func() error {
			return detectComposeDeviceDrift(
				map[string]map[string]string{"eth0": {"type": "nic", "network": "new"}},
				map[string]map[string]string{"eth0": {"type": "nic", "network": "old"}})
		}, ComposeDriftDevice},
		{"missing device", func() error {
			return detectComposeDeviceDrift(map[string]map[string]string{"cache": {"type": "disk", "path": "/cache"}}, nil)
		}, ComposeDriftDevice},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var typed *ComposeDriftError
			if err := tc.drift(); !errors.As(err, &typed) || typed.Kind != tc.kind {
				t.Fatalf("expected %s drift; got %v", tc.kind, err)
			}
		})
	}
}

func TestStampComposeCommandsDetectsChangesWithoutExposingValues(t *testing.T) {
	makeConfig := func(command string) composeclient.InstanceConfig {
		return composeclient.InstanceConfig{Command: []string{"sh", "-c", command}}
	}
	first := makeConfig("echo secret-one")
	unchanged := makeConfig("echo secret-one")
	changed := makeConfig("echo secret-two")
	for _, cfg := range []*composeclient.InstanceConfig{&first, &unchanged, &changed} {
		if err := stampComposeCommands(cfg); err != nil {
			t.Fatal(err)
		}
	}
	a := first.Extensions[ownershipCommandKey]
	if a == "" || a != unchanged.Extensions[ownershipCommandKey] || a == changed.Extensions[ownershipCommandKey] {
		t.Fatalf("command digest not stable or fails to detect change")
	}
	if strings.Contains(a, "secret") {
		t.Fatal("command marker leaked command contents")
	}
	if err := detectComposeInstanceDrift(changed.Extensions, nil, first.Extensions, nil); err == nil {
		t.Fatal("command digest change must register as configuration drift")
	}
	entrypoint := makeConfig("echo secret-one")
	entrypoint.Entrypoint = []string{"/bin/sh"}
	if err := stampComposeCommands(&entrypoint); err != nil {
		t.Fatal(err)
	}
	if entrypoint.Extensions[ownershipCommandKey] == a {
		t.Fatal("entrypoint change was not detected")
	}
}
func TestStampComposeCommandsRejectsReservedMarker(t *testing.T) {
	cfg := composeclient.InstanceConfig{Extensions: map[string]string{ownershipCommandKey: "overridden"}}
	if err := stampComposeCommands(&cfg); err == nil {
		t.Fatal("accepted user-controlled command marker")
	}
}

func TestComposeValidationUsesSafeNonemptyInterpolation(t *testing.T) {
	environment := validationEnvironment()
	for _, key := range []string{
		"AGINCTUS_GATEWAY_POSTGRES_PASSWORD",
		"AGINCTUS_GATEWAY_MASTER_KEY",
		"AGINCTUS_GATEWAY_SALT_KEY",
	} {
		if environment[key] == "" {
			t.Fatalf("missing validation placeholder for %s", key)
		}
	}
	raw := json.RawMessage(`{"project":"aginctus-test","compose":{"services":{"postgres":{"image":"postgres:latest","environment":{"POSTGRES_PASSWORD":"${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}"}},"litellm":{"image":"alpine:latest","environment":{"LITELLM_MASTER_KEY":"${AGINCTUS_GATEWAY_MASTER_KEY}","LITELLM_SALT_KEY":"${AGINCTUS_GATEWAY_SALT_KEY}"}}}}}`)
	if err := New().Validate(raw); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestComposeExecutionGetsRuntimeEnvironment(t *testing.T) {
	environment := map[string]string{
		"AGINCTUS_GATEWAY_POSTGRES_PASSWORD": "real-test-password",
		"AGINCTUS_GATEWAY_MASTER_KEY":        "real-test-key",
		"AGINCTUS_GATEWAY_SALT_KEY":          "real-test-salt",
	}
	d := NewWithEnvironment(environment)
	called := false
	d.execute = func(_ context.Context, _ Config, got map[string]string) error {
		called = true
		for k, want := range environment {
			if got[k] != want {
				t.Errorf("%s not passed to execution", k)
			}
		}
		return nil
	}
	raw := json.RawMessage(`{"project":"aginctus-test","compose":{"services":{"web":{"image":"alpine:latest","environment":{"TOKEN":"${AGINCTUS_GATEWAY_MASTER_KEY}"}}}}}`)
	if err := d.Validate(raw); err != nil {
		t.Fatal(err)
	}
	if err := d.Execute(context.Background(), raw, orchestration.ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("execution not invoked")
	}
}

// In a crashed OCI-image import, upstream's temporary container may remain
// when the next reconcile inventories an otherwise owned Compose project.
func TestComposeInstanceInventoryAcceptsInterruptedImport(t *testing.T) {
	const project = "gateway-test"
	owned := api.InstanceFull{Instance: api.Instance{
		Name: "litellm-1", Type: string(api.InstanceTypeContainer),
		InstancePut: api.InstancePut{Config: map[string]string{
			ownershipManagedKey:    "true",
			ownershipResourceKey:   "compose-instance",
			ownershipDeploymentKey: project,
		}},
	}}
	seed := api.InstanceFull{Instance: api.Instance{
		Name: "ic-seed-abcdef", Type: string(api.InstanceTypeContainer),
		InstancePut: api.InstancePut{Config: map[string]string{
			composeclient.TempInstanceKey: "true",
		}},
	}}
	if !isComposeImageSeed(seed) {
		t.Fatal("upstream temporary image reader not identified")
	}
	if err := verifyComposeInstanceInventory([]api.InstanceFull{owned, seed}, project); err != nil {
		t.Fatalf("owned services plus interrupted image seed rejected: %v", err)
	}
	if err := verifyComposeInstanceInventory([]api.InstanceFull{owned}, project); err != nil {
		t.Fatalf("owned service inventory rejected: %v", err)
	}
}

func TestComposeInstanceInventoryRejectsForeignSeedsAndServices(t *testing.T) {
	const project = "gateway-test"
	makeInstance := func(name, kind string, config map[string]string) api.InstanceFull {
		return api.InstanceFull{Instance: api.Instance{
			Name: name, Type: kind, InstancePut: api.InstancePut{Config: config},
		}}
	}
	seedMarker := map[string]string{composeclient.TempInstanceKey: "true"}
	cases := []struct {
		name     string
		instance api.InstanceFull
	}{
		{"unmarked seed prefix", makeInstance("ic-seed-abcdef", string(api.InstanceTypeContainer), nil)},
		{"seed with false marker", makeInstance("ic-seed-abcdef", string(api.InstanceTypeContainer), map[string]string{composeclient.TempInstanceKey: "false"})},
		{"seed prefix but VM", makeInstance("ic-seed-abcdef", string(api.InstanceTypeVM), seedMarker)},
		{"empty seed suffix", makeInstance("ic-seed-", string(api.InstanceTypeContainer), seedMarker)},
		{"embedded seed prefix", makeInstance("custom-ic-seed-abcdef", string(api.InstanceTypeContainer), seedMarker)},
		{"bare seed name", makeInstance("ic-seed", string(api.InstanceTypeContainer), seedMarker)},
		{"ordinary unowned service", makeInstance("postgres-1", string(api.InstanceTypeContainer), nil)},
		{"wrong deployment service", makeInstance("litellm-1", string(api.InstanceTypeContainer), map[string]string{
			ownershipManagedKey: "true", ownershipResourceKey: "compose-instance", ownershipDeploymentKey: "other",
		})},
		{"seed with unexpected ownership marker", makeInstance("ic-seed-abcdef", string(api.InstanceTypeContainer), map[string]string{
			composeclient.TempInstanceKey: "true", ownershipResourceKey: "foreign",
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isComposeImageSeed(tc.instance) {
				t.Fatal("foreign instance incorrectly recognized as upstream image seed")
			}
			if err := verifyComposeInstanceInventory([]api.InstanceFull{tc.instance}, project); err == nil {
				t.Fatal("foreign instance passed deployment ownership validation")
			}
		})
	}
}

func TestComposeInstanceInventoryRejectsForeignAmongOwnedAndSeed(t *testing.T) {
	const project = "gateway-test"
	owned := api.InstanceFull{Instance: api.Instance{
		Name: "postgres-1", Type: string(api.InstanceTypeContainer),
		InstancePut: api.InstancePut{Config: map[string]string{
			ownershipManagedKey:    "true",
			ownershipResourceKey:   "compose-instance",
			ownershipDeploymentKey: project,
		}},
	}}
	seed := api.InstanceFull{Instance: api.Instance{
		Name: "ic-seed-abcdef", Type: string(api.InstanceTypeContainer),
		InstancePut: api.InstancePut{Config: map[string]string{
			composeclient.TempInstanceKey: "true",
		}},
	}}
	foreign := api.InstanceFull{Instance: api.Instance{
		Name: "unexpected-1", Type: string(api.InstanceTypeContainer),
	}}
	if err := verifyComposeInstanceInventory([]api.InstanceFull{owned, seed, foreign}, project); err == nil {
		t.Fatal("foreign service accepted after legitimate image seed")
	}
}
