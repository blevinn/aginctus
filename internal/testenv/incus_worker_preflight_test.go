package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type workerInspectorFake struct {
	*fakeIncusVMClient
	pool      WorkerPoolInspection
	imageInfo WorkerImageInspection
	err       error
}

func TestWorkerPreflightNativeObservationsAreScopedReadOnly(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	requests := 0
	n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodGet || r.URL.Query().Get("project") != DefaultVMProject {
			t.Fatal("unexpected authority or mutation", r.Method, r.URL)
		}
		metadata := ""
		switch r.URL.Path {
		case "/1.0/storage-pools/testenv":
			metadata = `{"name":"testenv","driver":"zfs","status":"Created","config":{"source":"private-device"}}`
		case "/1.0/storage-pools/testenv/resources":
			metadata = `{"space":{"total":77309411328,"used":1024}}`
		case "/1.0/images/" + fingerprint:
			metadata = `{"fingerprint":"` + fingerprint + `","project":"aginctus-testenv","type":"virtual-machine","architecture":"x86_64"}`
		default:
			t.Fatal("unexpected endpoint", r.URL)
		}
		return testIncusResponse(`{"type":"sync","status_code":200,"metadata":` + metadata + `}`), nil
	}))
	pool, err := n.InspectWorkerPool(context.Background())
	if err != nil || pool.Name != "testenv" || pool.Total != 72*1024*1024*1024 || pool.Used != 1024 {
		t.Fatal(pool, err)
	}
	image, err := n.InspectWorkerImage(context.Background(), fingerprint)
	if err != nil || image.Project != DefaultVMProject || image.Type != "virtual-machine" || image.Fingerprint != fingerprint || requests != 3 {
		t.Fatal(image, err, requests)
	}
	if _, err := n.InspectWorkerImage(context.Background(), "mutable-alias"); err == nil || requests != 3 {
		t.Fatal("queried alias")
	}
}

func TestWorkerPreflightNativeRequestsBoundCancellation(t *testing.T) {
	for _, image := range []bool{false, true} {
		n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		var err error
		if image {
			_, err = n.InspectWorkerImage(ctx, strings.Repeat("a", 64))
		} else {
			_, err = n.InspectWorkerPool(ctx)
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("lost deadline", err)
		}
	}
}

func (f *workerInspectorFake) InspectWorkerPool(context.Context) (WorkerPoolInspection, error) {
	return f.pool, f.err
}
func (f *workerInspectorFake) InspectWorkerImage(context.Context, string) (WorkerImageInspection, error) {
	return f.imageInfo, f.err
}

func workerPreflightFixture(t *testing.T) (*workerInspectorFake, *IncusVMBackend, TrustedIncusPreflightOptions) {
	t.Helper()
	c := &workerInspectorFake{fakeIncusVMClient: newFakeIncusClient(t)}
	c.pool = WorkerPoolInspection{Name: "testenv", Driver: "zfs", Status: "Created", Total: 72 * 1024 * 1024 * 1024,
		Config: map[string]string{"source": "private-operator-device", "size": "72GiB"}}
	c.imageInfo = WorkerImageInspection{Fingerprint: c.image, Project: DefaultVMProject, Type: "virtual-machine", Architecture: "x86_64"}
	b, err := newTestIncusBackend(c, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	cfg := TrustedIncusPreflightOptions{SocketPath: "/var/lib/incus/unix.socket", ImageFingerprint: c.image, InspectPool: true}
	r, err := checkIncusWorkerPreflight(context.Background(), b, cfg)
	if err != nil || r.Status != "inspection_only" {
		t.Fatal(r, err)
	}
	cfg.InspectPool, cfg.PoolConfigFingerprint = false, r.PoolConfigFingerprint
	return c, b, cfg
}

func TestWorkerPreflightReadOnlyAndConservative(t *testing.T) {
	c, b, cfg := workerPreflightFixture(t)
	for _, driver := range []string{"zfs", "lvm"} {
		c.pool.Driver = driver
		cfg.InspectPool = true
		candidate, err := checkIncusWorkerPreflight(context.Background(), b, cfg)
		if err != nil || candidate.Status != "inspection_only" {
			t.Fatal(candidate, err)
		}
		cfg.InspectPool, cfg.PoolConfigFingerprint = false, candidate.PoolConfigFingerprint
		r, err := checkIncusWorkerPreflight(context.Background(), b, cfg)
		if err != nil || r.Status != "pass" {
			t.Fatal(r, err)
		}
		for _, check := range r.Results {
			if (check.ID == "cleanup" || check.ID == "crash_reboot" || check.ID == "resource_enforcement") && check.Status != "not_verified" {
				t.Fatal("metadata inspection promoted to runtime evidence", check)
			}
		}
		data, err := json.Marshal(r)
		if err != nil || len(data) > 4096 || strings.Contains(string(data), "private-operator-device") {
			t.Fatal("unbounded/raw metadata", string(data), err)
		}
	}
	if len(c.calls) != 0 || len(c.vms) != 0 {
		t.Fatal("read-only preflight mutated daemon", c.calls)
	}
}

func TestWorkerPreflightRejectsDriftBeforeMutation(t *testing.T) {
	changes := map[string]func(*workerInspectorFake, *TrustedIncusPreflightOptions){
		"socket": func(_ *workerInspectorFake, c *TrustedIncusPreflightOptions) { c.SocketPath = "/tmp/pilot.socket" },
		"alias":  func(_ *workerInspectorFake, c *TrustedIncusPreflightOptions) { c.ImageFingerprint = "mutable-alias" },
		"pool-pin": func(_ *workerInspectorFake, c *TrustedIncusPreflightOptions) {
			c.PoolConfigFingerprint = strings.Repeat("0", 64)
		},
		"project": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) {
			f.policy["restricted.devices.nic"] = "allow"
		},
		"pool-name":   func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.pool.Name = "pilot" },
		"pool-driver": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.pool.Driver = "dir" },
		"pool-status": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.pool.Status = "Unavailable" },
		"pool-low":    func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.pool.Total = 1 },
		"pool-high":   func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.pool.Total = 81 * 1024 * 1024 * 1024 },
		"pool-used":   func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.pool.Used = f.pool.Total + 1 },
		"pool-config": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) {
			f.pool.Config["source"] = "different-device"
		},
		"pool-metadata-size": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) {
			f.pool.Config["user.unbounded"] = strings.Repeat("x", 65536)
		},
		"image-digest": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) {
			f.imageInfo.Fingerprint = strings.Repeat("0", 64)
		},
		"image-project": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.Project = "default" },
		"image-type":    func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.Type = "container" },
		"image-arch":    func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.Architecture = "aarch64" },
		"image-public":  func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.Public = true },
		"image-updates": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.AutoUpdate = true },
		"image-cached":  func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.Cached = true },
		"image-remote":  func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) { f.imageInfo.RemoteSource = true },
		"daemon-error": func(f *workerInspectorFake, _ *TrustedIncusPreflightOptions) {
			f.err = errors.New("private-daemon-error")
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c, b, cfg := workerPreflightFixture(t)
			change(c, &cfg)
			r, err := checkIncusWorkerPreflight(context.Background(), b, cfg)
			if err == nil || r.Status != "fail" {
				t.Fatal("accepted drift", r, err)
			}
			if len(c.calls) != 0 {
				t.Fatal("preflight mutated daemon", c.calls)
			}
		})
	}
	_, b, cfg := workerPreflightFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := checkIncusWorkerPreflight(ctx, b, cfg); err == nil || r.Status != "fail" {
		t.Fatal(r, err)
	}
}
