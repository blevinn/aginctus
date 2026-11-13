package native

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeGatewayAliasClient struct {
	info        *incus.ConnectionInfo
	project     *api.Project
	image       *api.Image
	alias       *api.ImageAliasesEntry
	creates     int
	createErr   error
	onCreate    func()
	aliasErr    error
	aliasReads  int
	onAliasRead func(int)
}

func TestGatewayAliasBindsAcquisitionToFullOCIIdentity(t *testing.T) {
	for _, field := range []string{"registry", "repository", "manifest", "config"} {
		t.Run(field, func(t *testing.T) {
			intent, acquisitionClient := imageAcquireFixture()
			acquisitionClient.onCreate = func() {
				acquisitionClient.image = &api.Image{Project: intent.Project, Type: "container", Fingerprint: intent.Image.IncusFingerprint}
			}
			acquired, err := AcquireGatewayImageWithClient(context.Background(), intent, acquisitionClient)
			if err != nil {
				t.Fatal(err)
			}
			in, client := gatewayAliasFixture()
			in.Acquisition = acquired
			switch field {
			case "registry":
				in.Image.Registry = "registry.example"
			case "repository":
				in.Image.Repository = "other/image"
			case "manifest":
				in.Image.ManifestDigest = "sha256:" + strings.Repeat("d", 64)
			case "config":
				in.Image.ConfigDigest = "sha256:" + strings.Repeat("d", 64)
			}
			_, err = CreateGatewayImageAliasWithClient(context.Background(), in, client)
			var typed *GatewayAliasCreateError
			if !errors.As(err, &typed) || typed.Stage != "intent" || typed.Attempted || client.creates != 0 {
				t.Fatalf("different OCI identity accepted with same layer fingerprint: %v creates=%d", err, client.creates)
			}
		})
	}
}

func (f *fakeGatewayAliasClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return f.info, nil
}
func (f *fakeGatewayAliasClient) GetProject(string) (*api.Project, string, error) {
	return f.project, "", nil
}
func (f *fakeGatewayAliasClient) GetImage(string) (*api.Image, string, error) {
	return f.image, "", nil
}
func (f *fakeGatewayAliasClient) GetImageAlias(string) (*api.ImageAliasesEntry, string, error) {
	f.aliasReads++
	if f.onAliasRead != nil {
		f.onAliasRead(f.aliasReads)
	}
	if f.aliasErr != nil {
		return nil, "", f.aliasErr
	}
	if f.alias == nil {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	return f.alias, "", nil
}

func TestGatewayAliasRejectsUnknownAbsenceAndBoundaryCollision(t *testing.T) {
	for _, mode := range []string{"denied", "unavailable", "boundary-collision"} {
		t.Run(mode, func(t *testing.T) {
			in, f := gatewayAliasFixture()
			if mode == "boundary-collision" {
				f.onAliasRead = func(read int) {
					if read == 2 {
						f.alias = &api.ImageAliasesEntry{Name: in.Alias}
					}
				}
			} else {
				code := http.StatusForbidden
				if mode == "unavailable" {
					code = http.StatusServiceUnavailable
				}
				f.aliasErr = api.StatusErrorf(code, "private daemon diagnostic")
			}
			out, err := CreateGatewayImageAliasWithClient(context.Background(), in, f)
			var typed *GatewayAliasCreateError
			if !errors.As(err, &typed) || typed.Attempted || typed.Uncertain || out.Created || f.creates != 0 {
				t.Fatalf("unverified alias absence allowed a write: %+v %v", out, err)
			}
		})
	}
}

func TestGatewayAliasCreateErrorIsUncertain(t *testing.T) {
	in, f := gatewayAliasFixture()
	f.createErr = errors.New("private daemon diagnostic")
	out, err := CreateGatewayImageAliasWithClient(context.Background(), in, f)
	var typed *GatewayAliasCreateError
	if !errors.As(err, &typed) || typed.Stage != "create" || !typed.Attempted || !typed.Uncertain ||
		out.Created || out.Verified || f.creates != 1 || strings.Contains(err.Error(), "private daemon diagnostic") {
		t.Fatalf("submission failure not safely reported: %+v %v", out, err)
	}
}
func (f *fakeGatewayAliasClient) CreateImageAlias(req api.ImageAliasesPost) error {
	f.creates++
	if f.createErr != nil {
		return f.createErr
	}
	f.alias = &req.ImageAliasesEntry
	if f.onCreate != nil {
		f.onCreate()
	}
	return nil
}
func gatewayAliasFixture() (GatewayAliasCreateIntent, *fakeGatewayAliasClient) {
	intent, _ := imageAcquireFixture()
	in := GatewayAliasCreateIntent{
		Project: intent.Project, Alias: "postgres-image", Image: intent.Image,
		Acquisition: GatewayImageAcquireResult{Project: intent.Project, Image: intent.Image,
			Fingerprint: intent.Image.IncusFingerprint, Attempted: true, Verified: true},
	}
	scope, _ := incusscope.NewProjectScope(intent.Project)
	return in, &fakeGatewayAliasClient{
		info:    &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/run/incus/unix.socket", Project: intent.Project},
		project: &api.Project{Name: intent.Project, ProjectPut: api.ProjectPut{Config: scope.DesiredConfig()}},
		image:   &api.Image{Project: intent.Project, Type: "container", Fingerprint: intent.Image.IncusFingerprint},
	}
}
func TestGatewayAliasCreatesAndVerifies(t *testing.T) {
	in, f := gatewayAliasFixture()
	out, err := CreateGatewayImageAliasWithClient(context.Background(), in, f)
	if err != nil || !out.Created || !out.Verified || f.creates != 1 {
		t.Fatalf("create: %+v %v", out, err)
	}
	if _, err := CreateGatewayImageAliasWithClient(context.Background(), in, f); err == nil || f.creates != 1 {
		t.Fatal("repeated create adopted existing alias")
	}
}
func TestGatewayAliasRejectsUntrustedInputsBeforeMutation(t *testing.T) {
	tests := map[string]func(*GatewayAliasCreateIntent, *fakeGatewayAliasClient){
		"unverified acquisition":  func(i *GatewayAliasCreateIntent, _ *fakeGatewayAliasClient) { i.Acquisition.Verified = false },
		"unattempted acquisition": func(i *GatewayAliasCreateIntent, _ *fakeGatewayAliasClient) { i.Acquisition.Attempted = false },
		"wrong project":           func(_ *GatewayAliasCreateIntent, f *fakeGatewayAliasClient) { f.info.Project = "default" },
		"foreign image":           func(_ *GatewayAliasCreateIntent, f *fakeGatewayAliasClient) { f.image.Project = "foreign" },
		"foreign project policy": func(_ *GatewayAliasCreateIntent, f *fakeGatewayAliasClient) {
			f.project.Config["user.aginctus.managed"] = "false"
		},
		"existing alias": func(i *GatewayAliasCreateIntent, f *fakeGatewayAliasClient) {
			f.alias = &api.ImageAliasesEntry{Name: i.Alias}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			in, f := gatewayAliasFixture()
			change(&in, f)
			if _, err := CreateGatewayImageAliasWithClient(context.Background(), in, f); err == nil || f.creates != 0 {
				t.Fatalf("unsafe alias write: %v", err)
			}
		})
	}
}
func TestGatewayAliasUncertainAfterSubmission(t *testing.T) {
	in, f := gatewayAliasFixture()
	f.onCreate = func() { f.alias = nil }
	out, err := CreateGatewayImageAliasWithClient(context.Background(), in, f)
	var typed *GatewayAliasCreateError
	if !errors.As(err, &typed) || !typed.Uncertain || !typed.Attempted || !out.Created || out.Verified {
		t.Fatalf("uncertain: %+v %v", out, err)
	}
}
func TestGatewayAliasCancellation(t *testing.T) {
	in, f := gatewayAliasFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CreateGatewayImageAliasWithClient(ctx, in, f)
	if !errors.Is(err, context.Canceled) || f.creates != 0 {
		t.Fatalf("cancel: %v", err)
	}
}

func TestGatewayAliasRejectsChangedTargetAfterCreate(t *testing.T) {
	in, f := gatewayAliasFixture()
	f.onCreate = func() { f.image.Project = "foreign" }
	result, err := CreateGatewayImageAliasWithClient(context.Background(), in, f)
	var typed *GatewayAliasCreateError
	if !errors.As(err, &typed) || !typed.Uncertain || !typed.Attempted ||
		!result.Created || result.Verified {
		t.Fatalf("changed image postflight: %+v %v", result, err)
	}
}
