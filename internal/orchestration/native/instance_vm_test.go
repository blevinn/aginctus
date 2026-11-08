package native

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

func requireInstanceVM(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("AGINCTUS_NATIVE_INSTANCE_VM") != "disposable-vm" {
		t.Skip("requires explicit disposable VM harness")
	}
	marker, err := os.ReadFile("/run/aginctus-native-instance-disposable")
	if err != nil || string(marker) != "native-instance-vm\n" {
		t.Fatal("disposable VM marker unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func TestNativeInstanceVMProvision(t *testing.T) {
	ctx := requireInstanceVM(t)
	scope, _ := incusscope.NewProjectScope("native-instance-vm")
	result, err := EnsureLocalProject(ctx, scope, ProjectCreateAuthorization{Confirmed: true})
	if err != nil || !result.Verified {
		t.Fatalf("project fixture: %+v %v", result, err)
	}
}

func TestNativeInstanceVMAcceptance(t *testing.T) {
	ctx := requireInstanceVM(t)
	server, err := incus.ConnectIncusUnixWithContext(ctx, "", nil)
	if err != nil {
		t.Fatal("guest socket unavailable")
	}
	defer server.Disconnect()
	server = server.UseProject("native-instance-vm")
	client := sdkInstanceEnsureClient{sdkInstanceUpdateClient{server: server}}
	intent := InstanceIntent{
		Identity: InstanceIdentity{Project: "native-instance-vm", Name: "selected"}, WorkloadID: "selected", Type: "container", ImageAlias: "fixture", Profiles: []string{}, Description: "before",
		Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "selected", "user.aginctus.deployment": "native-instance-vm", "user.unmanaged": "sentinel"},
		Devices: map[string]map[string]string{
			"root":       {"type": "disk", "path": "/", "pool": "default"},
			"management": {"type": "nic", "network": "native-mgmt", "name": "eth0"},
			"state":      {"type": "disk", "pool": "default", "source": "protected", "path": "/state"},
			"workspace":  {"type": "disk", "source": "/srv/workspace", "path": "/workspace", "readonly": "true"},
		},
	}
	get := func() (*api.Instance, string) {
		t.Helper()
		i, etag, err := server.GetInstance(intent.Identity.Name)
		if err != nil {
			t.Fatal("instance read failed")
		}
		return i, etag
	}
	preview, err := EnsureInstanceWithClient(ctx, intent, true, client)
	if err != nil || preview.Plan.Action != Create {
		t.Fatalf("create preview: %+v %v", preview, err)
	}
	if _, _, err := server.GetInstance(intent.Identity.Name); !api.StatusErrorCheck(err, 404) {
		t.Fatal("dry-run created instance")
	}
	if _, err := ensureInstanceWithClient(ctx, intent, false, false, client); err == nil {
		t.Fatal("existing-only mode accepted absence")
	}
	created, err := EnsureInstanceWithClient(ctx, intent, false, client)
	if err != nil || !created.Created || !created.Verified {
		t.Fatalf("create: %+v %v", created, err)
	}
	before, staleETag := get()
	if before.Status != "Stopped" {
		t.Fatal("create unexpectedly started instance")
	}
	// Omitted fields and attachments must survive the full SDK update.
	intent.Config = maps.Clone(intent.Config)
	delete(intent.Config, "user.unmanaged")
	delete(intent.Devices, "state")
	delete(intent.Devices, "workspace")
	intent.Description = "after"
	intent.Config["user.changed"] = "updated"
	intent.Config["user.explicit-empty"] = ""
	preview, err = EnsureInstanceWithClient(ctx, intent, true, client)
	afterPreview, previewETag := get()
	if err != nil || preview.Plan.Action != Update || staleETag != previewETag || !reflect.DeepEqual(before, afterPreview) {
		t.Fatal("dry-run mutated observed instance")
	}
	updated, err := ensureInstanceWithClient(ctx, intent, false, false, client)
	if err != nil || !updated.Updated || !updated.Verified {
		t.Fatalf("update: %+v %v", updated, err)
	}
	after, _ := get()
	if !reflect.DeepEqual(before.Devices, after.Devices) || after.Config["user.unmanaged"] != "sentinel" || before.Architecture != after.Architecture || before.Ephemeral != after.Ephemeral || before.Stateful != after.Stateful {
		t.Fatal("update lost unmanaged writable data")
	}
	if op, err := server.UpdateInstance(intent.Identity.Name, before.Writable(), staleETag); err == nil {
		if op != nil {
			_ = op.WaitContext(ctx)
		}
		t.Fatal("daemon accepted stale ETag")
	}
	intent.EnsureRunning = true
	started, err := EnsureInstanceWithClient(ctx, intent, false, client)
	if err != nil || !started.Started || !started.Verified {
		t.Fatalf("start: %+v %v", started, err)
	}
	if err := server.CreateInstanceFile(intent.Identity.Name, "/state/sentinel", incus.InstanceFileArgs{Content: strings.NewReader("volume-sentinel"), Mode: 0600, Type: "file", WriteMode: "overwrite"}); err != nil {
		t.Fatal("volume sentinel write failed")
	}
	readSentinel := func(path, want string) {
		t.Helper()
		file, _, err := server.GetInstanceFile(intent.Identity.Name, path)
		if err != nil {
			t.Fatal("sentinel read failed")
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil || string(content) != want {
			t.Fatal("sentinel content changed")
		}
	}
	readSentinel("/workspace/sentinel", "workspace-sentinel")
	readSentinel("/state/sentinel", "volume-sentinel")
	waitBoot := func() {
		t.Helper()
		readyCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		for {
			op, err := server.ExecInstance(intent.Identity.Name, api.InstanceExecPost{Command: []string{"/run/current-system/sw/bin/systemctl", "is-system-running", "--wait"}}, nil)
			if err == nil && op != nil && op.WaitContext(readyCtx) == nil {
				return
			}
			select {
			case <-readyCtx.Done():
				t.Fatal("boot readiness unavailable")
			case <-time.After(time.Second):
			}
		}
	}
	waitBoot()
	for range 2 {
		repeated, err := EnsureInstanceWithClient(ctx, intent, false, client)
		if err != nil || repeated.Created || repeated.Updated || repeated.Started || !repeated.Verified {
			t.Fatalf("repeated ensure: %+v %v", repeated, err)
		}
	}
	scope, _ := incusscope.NewProjectScope(intent.Identity.Project)
	stop := InstanceLifecycleIntent{Scope: scope, Name: intent.Identity.Name, Resource: "workload", Role: "agent-workload", WorkloadID: intent.WorkloadID, State: InstanceStopped}
	for n := range 2 {
		stopped, err := EnsureInstanceStateWithClient(ctx, stop, client)
		if err != nil || !stopped.Verified || stopped.Changed != (n == 0) {
			t.Fatalf("stop: %+v %v", stopped, err)
		}
	}
	// Start again to prove data survives graceful stop and repeated ensure.
	if restarted, err := EnsureInstanceWithClient(ctx, intent, false, client); err != nil || !restarted.Verified {
		t.Fatalf("restart: %v", err)
	}
	readSentinel("/state/sentinel", "volume-sentinel")
	readSentinel("/workspace/sentinel", "workspace-sentinel")
	waitBoot()
	if _, err := EnsureInstanceStateWithClient(ctx, stop, client); err != nil {
		t.Fatal(err)
	}
	// Unsafe drift and cancellation must leave the selected instance unchanged.
	stable, stableETag := get()
	for _, drift := range []string{"device", "profile", "image", "ownership", "deployment"} {
		bad := intent
		bad.Config = maps.Clone(intent.Config)
		bad.Devices = cloneDevices(intent.Devices)
		switch drift {
		case "device":
			bad.Devices["root"]["pool"] = "other"
		case "profile":
			bad.Profiles = []string{"default"}
		case "image":
			bad.ImageAlias = "missing"
		case "ownership":
			bad.WorkloadID = "foreign"
			bad.Config["user.aginctus.workload"] = "foreign"
		case "deployment":
			bad.Config["user.aginctus.deployment"] = "foreign"
		}
		if _, err := EnsureInstanceWithClient(ctx, bad, false, client); err == nil {
			t.Fatalf("accepted %s drift", drift)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := EnsureInstanceWithClient(canceled, intent, false, client); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	final, finalETag := get()
	if stableETag != finalETag || !reflect.DeepEqual(stable, final) {
		t.Fatal("refused operations changed instance")
	}
}
