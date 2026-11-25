package nativelifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/herdrclient"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/blevinn/aginctus/internal/workload"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const fixtureProject = "team"

type fixtureOperation struct {
	incus.Operation
	wait func() error
}

func (o fixtureOperation) WaitContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return o.wait()
}

type projectFixture struct {
	t            *testing.T
	info         incus.ConnectionInfo
	instances    map[string]api.Instance
	volume       *api.StorageVolume
	fingerprint  string
	submissions  []string
	snapshots    []string
	backups      []string
	failWait     bool
	allowDefault bool
}

func (p *projectFixture) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	i := p.info
	return &i, nil
}
func (p *projectFixture) GetServer() (*api.Server, string, error) {
	return &api.Server{ServerUntrusted: api.ServerUntrusted{Auth: "trusted"}}, "server", nil
}
func (p *projectFixture) GetProject(name string) (*api.Project, string, error) {
	if name == "default" && !p.allowDefault {
		return nil, "", api.StatusErrorf(http.StatusForbidden, "denied")
	}
	if name != fixtureProject && name != "default" {
		p.t.Fatalf("unexpected project %s", name)
	}
	return &api.Project{Name: name, ProjectPut: api.ProjectPut{Config: incusscope.ProjectScope{Name: name}.DesiredConfig()}}, "project", nil
}
func (p *projectFixture) GetImageAlias(string) (*api.ImageAliasesEntry, string, error) {
	p.t.Fatal("pinned operation resolved an image alias")
	return nil, "", nil
}
func (p *projectFixture) GetImage(handle string) (*api.Image, string, error) {
	if handle != p.fingerprint {
		p.t.Fatalf("unexpected fingerprint %q", handle)
	}
	return &api.Image{Fingerprint: handle, Project: fixtureProject, Type: "container"}, "image", nil
}
func (p *projectFixture) GetInstance(name string) (*api.Instance, string, error) {
	if name != "agent-1" && name != "herdr-1" {
		p.t.Fatalf("read outside selected instances: %s", name)
	}
	i, ok := p.instances[name]
	if !ok {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	return &i, "instance", nil
}
func (p *projectFixture) CreateInstance(post api.InstancesPost) (incus.Operation, error) {
	p.submissions = append(p.submissions, "create:"+post.Name)
	if post.Source.Fingerprint != p.fingerprint || post.Type != api.InstanceTypeContainer || len(post.Profiles) != 0 || post.Config["user.aginctus.environment-id"] == "" {
		p.t.Fatalf("unsafe create: %+v", post)
	}
	if post.Name == "herdr-1" && (p.volume == nil || post.Devices["state_system"]["source"] != p.volume.Name || post.Devices["state_system"]["path"] != "/var/lib/aginctus/state/system") {
		p.t.Fatalf("Herdr created without its identity disk: %+v", post.Devices)
	}
	return fixtureOperation{wait: func() error {
		post.Config["volatile.base_image"] = post.Source.Fingerprint
		i := api.Instance{Name: post.Name, Project: fixtureProject, Type: string(post.Type), Status: "Stopped", CreatedAt: time.Now(), InstancePut: post.InstancePut, ExpandedDevices: post.Devices}
		p.instances[post.Name] = i
		if post.Name == "herdr-1" {
			p.volume.UsedBy = []string{"/1.0/instances/herdr-1?project=team"}
		}
		if p.failWait {
			return errors.New("lost operation evidence")
		}
		return nil
	}}, nil
}
func (p *projectFixture) UpdateInstance(name string, put api.InstancePut, etag string) (incus.Operation, error) {
	p.submissions = append(p.submissions, "update:"+name)
	if etag != "instance" || !reflect.DeepEqual(put.Devices, p.instances[name].Devices) || put.Config["operator.setting"] != "untouched" {
		p.t.Fatalf("update lost ETag, workspace, or unmanaged settings")
	}
	return fixtureOperation{wait: func() error {
		i := p.instances[name]
		i.InstancePut = put
		p.instances[name] = i
		return nil
	}}, nil
}
func (p *projectFixture) UpdateInstanceState(name string, put api.InstanceStatePut, etag string) (incus.Operation, error) {
	p.submissions = append(p.submissions, put.Action+":"+name)
	if put.Force || put.Stateful || etag != "instance" {
		p.t.Fatalf("unsafe lifecycle request %+v with etag %q", put, etag)
	}
	return fixtureOperation{wait: func() error {
		i := p.instances[name]
		i.Status = "Running"
		if put.Action == "stop" {
			i.Status = "Stopped"
		}
		p.instances[name] = i
		return nil
	}}, nil
}
func (p *projectFixture) GetInstanceSnapshotNames(string) ([]string, error) { return p.snapshots, nil }
func (p *projectFixture) GetInstanceBackupNames(string) ([]string, error)   { return p.backups, nil }
func (p *projectFixture) DeleteInstance(name string) (incus.Operation, error) {
	p.submissions = append(p.submissions, "delete:"+name)
	if p.instances[name].Status != "Stopped" {
		p.t.Fatal("deleted running instance")
	}
	return fixtureOperation{wait: func() error {
		delete(p.instances, name)
		if name == "herdr-1" {
			p.volume.UsedBy = nil
		}
		return nil
	}}, nil
}
func (p *projectFixture) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	if pool != "approved-pool" || kind != "custom" || name != "herdr-1-state-system" {
		p.t.Fatalf("unexpected volume read: %s %s %s", pool, kind, name)
	}
	if p.volume == nil {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	return p.volume, "volume", nil
}
func (p *projectFixture) CreateStoragePoolVolume(pool string, post api.StorageVolumesPost) error {
	p.submissions = append(p.submissions, "create:"+post.Name)
	if pool != "approved-pool" || p.volume != nil {
		p.t.Fatal("attempted volume overwrite or retargeting")
	}
	p.volume = &api.StorageVolume{StorageVolumePut: post.StorageVolumePut, Name: post.Name, Type: post.Type, ContentType: post.ContentType, Project: fixtureProject, CreatedAt: time.Now()}
	return nil
}

type bridgeFixture struct{ network api.Network }

func (b bridgeFixture) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/fixture/incus.socket", Project: "default"}, nil
}
func (b bridgeFixture) GetNetwork(name string) (*api.Network, string, error) {
	if name != b.network.Name {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	i := b.network
	return &i, "network", nil
}

type runFixture struct {
	project   *projectFixture
	accepted  environment.Candidate
	revision  environment.AcceptedRevision
	selection Selection
	pin       string
	verify    func(context.Context, environment.Descriptor, string, string, string) error
}

func newFixture(t *testing.T) runFixture {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	cert := srv.Certificate().Raw
	srv.Close()
	pin := fmt.Sprintf("%x", sha256.Sum256(cert))
	p := &projectFixture{t: t, instances: map[string]api.Instance{}, fingerprint: strings.Repeat("a", 64), info: incus.ConnectionInfo{
		Protocol: "incus", Project: fixtureProject, URL: "https://fixture.invalid", Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})),
	}}
	accepted := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind,
		Environment:       environment.Descriptor{ID: "c03124df-64c1-49c9-ad0a-32b61547f657", Name: "fixture", Binding: environment.Binding{Project: fixtureProject, ServerID: "sha256:" + pin}},
		SelectedWorkloads: []string{"agent", "omitted"},
		Workloads: []acceptedstate.WorkloadSpec{
			{ID: "agent", InstanceName: "agent-1", InstanceType: "container", ImageFingerprint: p.fingerprint, Profiles: []string{}, EnsureRunning: true},
			{ID: "omitted", InstanceName: "omitted-1", InstanceType: "container", ImageFingerprint: p.fingerprint, Profiles: []string{}, EnsureRunning: true},
		},
	}
	revision, err := environment.BuildAcceptedRevision(nil, nil, accepted)
	if err != nil {
		t.Fatal(err)
	}
	spec := managementnetwork.Spec{Name: "approved-bridge", IPv4Address: "auto", IPv4DHCPGateway: "none", IPv6Address: "none"}
	intent, err := spec.NativeIntent()
	if err != nil {
		t.Fatal(err)
	}
	config := intent.Scope.Ownership()
	maps.Copy(config, intent.Config)
	network := bridgeFixture{api.Network{Name: spec.Name, Type: "bridge", Managed: true, Project: "default", NetworkPut: api.NetworkPut{Config: config}}}
	selection := Selection{
		Bridge: &ManagementNetwork{Spec: spec, Reader: network, VerifyBinding: func(_ context.Context, binding environment.Binding, got managementnetwork.Spec) error {
			if binding != accepted.Environment.Binding || got != spec {
				t.Fatal("bridge binding redirected")
			}
			return nil
		}},
		Workloads: []Workload{{Spec: workload.Spec{ID: "agent", Name: "agent-1", Isolation: "container", Runtime: "opencode", StoragePool: "approved-pool", ManagementNetwork: spec.Name}, Operation: native.ReconcileInstance}},
		Herdr:     &Herdr{Spec: herdrclient.Spec{Name: "herdr-1", StoragePool: "approved-pool", ManagementNetwork: spec.Name}, ImageFingerprint: p.fingerprint, Operation: native.ReconcileInstance, CreateState: true},
	}
	return runFixture{p, accepted, revision, selection, pin, func(_ context.Context, env environment.Descriptor, name, pool, bridge string) error {
		if !env.SameEnvironment(accepted.Environment) || pool != "approved-pool" || bridge != "approved-bridge" || name != "agent-1" && name != "herdr-1" {
			t.Fatal("attachment binding redirected")
		}
		return nil
	}}
}

func (f runFixture) execute(t *testing.T, dry bool, guard func(orchestration.NativeEffect) error) (orchestration.Result, error) {
	t.Helper()
	plan, bindings, err := Build(f.accepted.Environment, f.revision, f.accepted, f.selection, f.pin, f.project, f.verify)
	if err != nil {
		t.Fatal(err)
	}
	return orchestration.NewEngine(nil).ExecuteNative(t.Context(), plan, orchestration.NativeBindings{Resources: bindings,
		VerifyPlan: func(_ context.Context, p orchestration.NativePlan) error {
			if p.Revision != f.revision || len(p.Resources) != len(plan.Resources) {
				t.Fatal("fixture authority received the wrong graph or pin")
			}
			return nil
		},
		BeforeEffect: func(_ context.Context, revision environment.AcceptedRevision, resource orchestration.NativeResource, effect orchestration.NativeEffect) error {
			if dry || resource.ID != effect.Target || revision != f.revision || resource.Kind == orchestration.NativeManagementNetwork {
				t.Fatal("wrong authority target, pin, or phase")
			}
			return guard(effect)
		},
		CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(_ context.Context, s orchestration.TargetScope, c check.Check) error {
			if s.Deployment != f.revision.EnvironmentID || c.Effect != check.ReadOnly {
				t.Fatal("unexpected check scope or effect")
			}
			return nil
		}},
	}, orchestration.ExecuteOptions{DryRun: dry})
}

func TestSelectedNativeLifecycleAndRetainedHerdrIdentity(t *testing.T) {
	f := newFixture(t)
	if r, err := f.execute(t, true, nil); err != nil || len(r.Native.Previews) != 4 || len(r.Conditions) != 0 || len(f.project.submissions) != 0 {
		t.Fatalf("dry-run observed or mutated wrong stages: %+v %v", r, err)
	}
	guard := func(orchestration.NativeEffect) error { return nil }
	r, err := f.execute(t, false, guard)
	if err != nil || len(r.Native.Effects) != 5 || len(r.Conditions) != 4 {
		t.Fatalf("initial lifecycle: %+v %v", r, err)
	}
	want := []string{"create:agent-1", "start:agent-1", "create:herdr-1-state-system", "create:herdr-1", "start:herdr-1"}
	if !reflect.DeepEqual(f.project.submissions, want) {
		t.Fatalf("effect order = %v; want %v", f.project.submissions, want)
	}
	for _, evidence := range r.Conditions {
		if evidence.Finding.AcceptedRevision != f.revision.ID || evidence.StepID != ManagementNetworkID && evidence.Finding.ResourceIncarnation == "" {
			t.Fatalf("unbound condition evidence: %+v", evidence)
		}
	}
	volume := f.project.volume
	if r, err := f.execute(t, false, guard); err != nil || len(r.Native.Effects) != 0 {
		t.Fatalf("reconcile not idempotent: %+v %v", r.Native, err)
	}
	agent := f.project.instances["agent-1"]
	agent.Config["operator.setting"] = "untouched"
	agent.Devices["workspace"] = map[string]string{"type": "disk", "source": "/outside/guest", "path": "/workspace"}
	agent.Description = "drift"
	f.project.instances["agent-1"] = agent
	if r, err := f.execute(t, false, guard); err != nil || len(r.Native.Effects) != 1 || r.Native.Effects[0].Effect.Action != native.Update {
		t.Fatalf("managed update did not retain workspace: %+v %v", r.Native, err)
	}
	f.selection.Bridge = nil
	f.selection.Herdr.CreateState = false
	for _, op := range []native.ProjectInstanceOperation{native.StopInstance, native.RemoveInstance, native.RemoveInstance} {
		f.selection.Workloads[0].Operation, f.selection.Herdr.Operation = op, op
		if _, err := f.execute(t, false, guard); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if len(f.project.instances) != 0 || f.project.volume != volume || len(f.project.submissions) != 10 {
		t.Fatalf("removal did not retain the exact identity volume or reapplied absent deletion: %v", f.project.submissions)
	}
}

func TestNativeLifecycleFailsClosed(t *testing.T) {
	for _, scenario := range []string{"denied-start", "lost-wait", "scope-revoked", "foreign-volume", "unknown-attachment"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			want := 0
			switch scenario {
			case "lost-wait":
				want, f.project.failWait = 1, true
			case "denied-start":
				want = 1
			case "foreign-volume":
				f.project.volume = &api.StorageVolume{Name: "herdr-1-state-system", Type: "custom", ContentType: "filesystem", Project: fixtureProject, CreatedAt: time.Now()}
			case "unknown-attachment":
				f.verify = func(_ context.Context, _ environment.Descriptor, name, _, _ string) error {
					if name == "herdr-1" {
						return errors.New("unverified Herdr attachment")
					}
					return nil
				}
			}
			r, err := f.execute(t, false, func(effect orchestration.NativeEffect) error {
				if scenario == "denied-start" && effect.Action == native.Start {
					return errors.New("revoked")
				}
				if scenario == "scope-revoked" {
					f.project.allowDefault = true
				}
				return nil
			})
			if err == nil || len(f.project.submissions) != want {
				t.Fatalf("expected fail closed after %d submissions; got %v: %v", want, f.project.submissions, err)
			}
			if scenario == "denied-start" && (len(r.Native.Effects) != 2 || r.Native.Effects[1].Outcome != orchestration.NativeEffectBlocked) ||
				scenario == "lost-wait" && (len(r.Native.Effects) != 1 || r.Native.Effects[0].Outcome != orchestration.NativeEffectUncertain) {
				t.Fatalf("lost partial evidence: %+v", r.Native)
			}
		})
	}
}

func TestNativeRemovalRefusesUnsafeInstance(t *testing.T) {
	for _, unsafe := range []string{"running", "ephemeral", "snapshot", "backup", "protected", "swapped-incarnation", "missing-herdr-volume", "herdr-disk-drift"} {
		t.Run(unsafe, func(t *testing.T) {
			f := newFixture(t)
			guard := func(orchestration.NativeEffect) error { return nil }
			if _, err := f.execute(t, false, guard); err != nil {
				t.Fatal(err)
			}
			f.project.submissions = nil
			f.selection.Bridge = nil
			f.selection.Workloads[0].Operation, f.selection.Herdr.Operation, f.selection.Herdr.CreateState = native.RemoveInstance, native.RemoveInstance, false
			agent, herdr := f.project.instances["agent-1"], f.project.instances["herdr-1"]
			agent.Status, herdr.Status = "Stopped", "Stopped"
			switch unsafe {
			case "running":
				agent.Status = "Running"
			case "ephemeral":
				agent.Ephemeral = true
			case "snapshot":
				f.project.snapshots = []string{"snap"}
			case "backup":
				f.project.backups = []string{"backup"}
			case "protected":
				agent.ExpandedConfig = map[string]string{"security.protection.delete": "true"}
			case "missing-herdr-volume":
				f.project.volume = nil
			case "herdr-disk-drift":
				delete(herdr.ExpandedDevices, "state_system")
			case "swapped-incarnation":
				guard = func(effect orchestration.NativeEffect) error {
					if effect.Target == "agent" {
						i := f.project.instances["agent-1"]
						i.CreatedAt = i.CreatedAt.Add(time.Second)
						f.project.instances["agent-1"] = i
					}
					return nil
				}
			}
			f.project.instances["agent-1"], f.project.instances["herdr-1"] = agent, herdr
			if _, err := f.execute(t, false, guard); err == nil || len(f.project.submissions) != 0 {
				t.Fatalf("unsafe removal: writes=%v error=%v", f.project.submissions, err)
			}
		})
	}
}
