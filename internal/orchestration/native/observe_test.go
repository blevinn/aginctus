package native

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeInstanceReader struct {
	intent     InstanceIntent
	observed   InstanceObservation
	mode       string
	reads      int
	aliasReads int
	onImage    func()
}

func (f *fakeInstanceReader) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	info := &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/test/incus.sock", Project: f.intent.Identity.Project}
	if f.mode == "remote" {
		info.SocketPath = ""
	}
	if f.mode == "ambient project" {
		info.Project = "default"
	}
	return info, nil
}

func (f *fakeInstanceReader) GetProject(name string) (*api.Project, string, error) {
	scope, _ := incusscope.NewProjectScope(name)
	config := scope.DesiredConfig()
	if f.mode == "foreign project" {
		delete(config, "user.aginctus.managed")
	}
	if f.mode == "project policy drift" {
		config["features.networks"] = "true"
	}
	return &api.Project{Name: name, ProjectPut: api.ProjectPut{Config: config}}, "project-etag", nil
}

func (f *fakeInstanceReader) GetInstance(name string) (*api.Instance, string, error) {
	f.reads++
	if f.mode == "absent" {
		return nil, "", errors.New("sensitive daemon diagnostics")
	}
	observed := f.observed
	if f.mode == "ownership" {
		observed.Config["user.aginctus.managed"] = "false"
	}
	instance := &api.Instance{
		Name: name, Project: observed.Identity.Project, Type: observed.Type, Status: observed.State, CreatedAt: time.Unix(100, 0),
		InstancePut: api.InstancePut{Description: observed.Description, Config: observed.Config, Devices: observed.Devices, Profiles: observed.Profiles},
	}
	etag := "instance-etag"
	if f.reads > 1 {
		switch f.mode {
		case "etag race":
			etag = "changed"
		case "state race":
			instance.Status = "Stopped"
		case "identity race":
			instance.CreatedAt = time.Unix(200, 0)
		}
	}
	return instance, etag, nil
}

func (f *fakeInstanceReader) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	f.aliasReads++
	alias := &api.ImageAliasesEntry{Name: name, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: f.observed.ImageFingerprint}}
	if f.mode == "alias race" && f.aliasReads > 1 {
		alias.Target = strings.Repeat("b", 64)
	}
	if f.mode == "wrong alias" {
		alias.Name = "foreign"
	}
	return alias, "alias-etag", nil
}

func (f *fakeInstanceReader) GetImage(fingerprint string) (*api.Image, string, error) {
	if f.onImage != nil {
		f.onImage()
		return nil, "", errors.New("sensitive canceled request")
	}
	image := &api.Image{Fingerprint: fingerprint, Project: f.intent.Identity.Project, Type: f.intent.Type}
	if f.mode == "foreign image" {
		image.Project = "foreign"
	}
	return image, "image-etag", nil
}

func TestInstanceObservationFailsClosed(t *testing.T) {
	for _, mode := range []string{"remote", "ambient project", "foreign project", "project policy drift", "absent", "ownership", "etag race", "state race", "identity race", "alias race", "wrong alias", "foreign image"} {
		t.Run(mode, func(t *testing.T) {
			intent, observed := instanceFixture()
			reader := &fakeInstanceReader{intent: intent, observed: observed, mode: mode}
			plan, err := PlanWithInstanceReader(context.Background(), intent, reader)
			if err == nil || plan.Action != "" || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("unsafe observation: %+v, %v", plan, err)
			}
		})
	}
	intent, observed := instanceFixture()
	reader := &fakeInstanceReader{intent: intent, observed: observed}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PlanWithInstanceReader(ctx, intent, reader); !errors.Is(err, context.Canceled) || reader.reads != 0 {
		t.Fatal("canceled plan read resources or reported success")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	reader.onImage = cancel
	if _, err := PlanWithInstanceReader(ctx, intent, reader); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("in-flight cancellation was lost or exposed daemon diagnostics")
	}
}

// Exercise real SDK query construction over a temporary Unix socket. This is
// hermetic wire evidence, not a disposable real-Incus acceptance test.
func TestInstanceObservationSDKUsesOnlyScopedLocalReads(t *testing.T) {
	intent, observed := instanceFixture()
	reader := &fakeInstanceReader{intent: intent, observed: observed}
	socket := filepath.Join(t.TempDir(), "incus.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Query().Get("project") != intent.Identity.Project {
			t.Errorf("unscoped or mutating SDK request: %s %s", r.Method, r.URL)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var value any
		var etag string
		switch r.URL.Path {
		case "/1.0/projects/aginctus":
			value, etag, _ = reader.GetProject("aginctus")
		case "/1.0/instances/dev":
			value, etag, _ = reader.GetInstance("dev")
		case "/1.0/images/aliases/aginctus-opencode":
			value, etag, _ = reader.GetImageAlias("aginctus-opencode")
		case "/1.0/images/" + observed.ImageFingerprint:
			value, etag, _ = reader.GetImage(observed.ImageFingerprint)
		default:
			t.Errorf("unexpected SDK read: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", etag)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status": "Success", "status_code": 200, "metadata": value})
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := incus.ConnectIncusUnixWithContext(ctx, socket, &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect()
	plan, err := PlanWithInstanceReader(ctx, intent, client.UseProject(intent.Identity.Project))
	if err != nil || plan.Action != Unchanged || plan.WriteAuthorized {
		t.Fatalf("unexpected SDK plan: %+v, %v", plan, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 7 {
		t.Fatalf("unexpected read sequence: %v", requests)
	}
}
