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

type fakeGatewayImageClient struct {
	info         *incus.ConnectionInfo
	project      *api.Project
	image        *api.Image
	reads        int
	creates      int
	op           GatewayImageAcquireOperation
	onCreate     func()
	request      api.ImagesPost
	globalImages []api.Image
	globalErr    error
	onGlobalRead func()
}

func (f *fakeGatewayImageClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return f.info, nil
}
func (f *fakeGatewayImageClient) GetProject(string) (*api.Project, string, error) {
	return f.project, "", nil
}
func (f *fakeGatewayImageClient) GetImage(string) (*api.Image, string, error) {
	f.reads++
	if f.image == nil {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	return f.image, "", nil
}
func (f *fakeGatewayImageClient) CreateImage(req api.ImagesPost) (GatewayImageAcquireOperation, error) {
	f.creates++
	f.request = req
	if f.onCreate != nil {
		f.onCreate()
	}
	return f.op, nil
}

type fakeImageAcquireOperation struct {
	metadata *api.Operation
	err      error
}

func (f fakeImageAcquireOperation) WaitContext(context.Context) error { return f.err }
func (f fakeImageAcquireOperation) Get() api.Operation                { return *f.metadata }
func (f *fakeGatewayImageClient) GetImagesAllProjectsWithFilter([]string) ([]api.Image, error) {
	if f.onGlobalRead != nil {
		f.onGlobalRead()
	}
	return f.globalImages, f.globalErr
}
func imageAcquireFixture() (GatewayImageAcquireIntent, *fakeGatewayImageClient) {
	project := "aginctus-test"
	scope, _ := incusscope.NewProjectScope(project)
	image := GatewayOCIIdentity{
		Registry: "docker.io", Repository: "library/postgres",
		ManifestDigest:   "sha256:" + strings.Repeat("a", 64),
		ConfigDigest:     "sha256:" + strings.Repeat("b", 64),
		IncusFingerprint: strings.Repeat("c", 64),
	}
	in := GatewayImageAcquireIntent{Project: project, Image: image}
	f := &fakeGatewayImageClient{
		info:    &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/run/incus/unix.socket", Project: project},
		project: &api.Project{Name: project, ProjectPut: api.ProjectPut{Config: scope.DesiredConfig()}},
		op:      fakeImageAcquireOperation{metadata: &api.Operation{Metadata: map[string]any{"fingerprint": image.IncusFingerprint}}},
	}
	return in, f
}
func TestGatewayImageRejectsPreexistingLayerFingerprint(t *testing.T) {
	in, f := imageAcquireFixture()
	f.image = &api.Image{Fingerprint: in.Image.IncusFingerprint}
	_, err := AcquireGatewayImageWithClient(context.Background(), in, f)
	if err == nil || f.creates != 0 {
		t.Fatalf("existing image adopted: %v", err)
	}
}
func TestGatewayImageCreatesImmutableSource(t *testing.T) {
	in, f := imageAcquireFixture()
	f.onCreate = func() {
		f.image = &api.Image{Fingerprint: in.Image.IncusFingerprint, Project: in.Project, Type: "container"}
	}
	result, err := AcquireGatewayImageWithClient(context.Background(), in, f)
	if err != nil || !result.Attempted || !result.Verified || result.Image != in.Image || f.creates != 1 {
		t.Fatalf("create: %+v %v", result, err)
	}
	source := f.request.Source
	if source == nil || source.Protocol != "oci" || source.Server != "https://docker.io" ||
		source.Alias != in.Image.Repository+"@"+in.Image.ManifestDigest || source.ImageType != "container" || f.request.AutoUpdate {
		t.Fatalf("non-immutable source: %+v", f.request)
	}
}

func TestGatewayImageRejectsDaemonWideCollisionAndUnknownInventory(t *testing.T) {
	for _, mode := range []string{"collision", "denied", "boundary"} {
		t.Run(mode, func(t *testing.T) {
			in, f := imageAcquireFixture()
			collision := []api.Image{{Fingerprint: in.Image.IncusFingerprint, Project: "other"}}
			switch mode {
			case "collision":
				f.globalImages = collision
			case "denied":
				f.globalErr = api.StatusErrorf(http.StatusForbidden, "private diagnostic")
			case "boundary":
				reads := 0
				f.onGlobalRead = func() {
					reads++
					if reads == 2 {
						f.globalImages = collision
					}
				}
			}
			result, err := AcquireGatewayImageWithClient(context.Background(), in, f)
			if err == nil || result.Attempted || f.creates != 0 {
				t.Fatalf("unsafe cache acquisition: %+v %v", result, err)
			}
		})
	}
}
func TestGatewayImageRejectsRemoteOrWrongProject(t *testing.T) {
	in, f := imageAcquireFixture()
	f.info.Project = "default"
	_, err := AcquireGatewayImageWithClient(context.Background(), in, f)
	if err == nil || f.creates != 0 {
		t.Fatalf("wrong project: %v", err)
	}
}
func TestGatewayImageReportsUncertainOperation(t *testing.T) {
	in, f := imageAcquireFixture()
	f.op = fakeImageAcquireOperation{err: context.DeadlineExceeded}
	result, err := AcquireGatewayImageWithClient(context.Background(), in, f)
	var typed *GatewayImageAcquireError
	if !errors.As(err, &typed) || !typed.Uncertain || !result.Attempted ||
		f.creates != 1 {
		t.Fatalf("uncertain: %+v %v", result, err)
	}
}
func TestGatewayImageRequiresCompleteIdentity(t *testing.T) {
	in, _ := imageAcquireFixture()
	in.Image.ConfigDigest = ""
	if err := in.Validate(); err == nil {
		t.Fatal("incomplete identity accepted")
	}
}

func TestGatewayImageRejectsMutableOrCredentialedSources(t *testing.T) {
	for _, registry := range []string{"user:password@registry.example", "registry.example/path", "registry.example?token=secret", "registry.example#fragment", "registry.example\\path"} {
		in, f := imageAcquireFixture()
		in.Image.Registry = registry
		if _, err := AcquireGatewayImageWithClient(context.Background(), in, f); err == nil || f.creates != 0 {
			t.Fatalf("invalid registry accepted: %q", registry)
		}
	}
	for _, repository := range []string{"library/postgres:latest", "library/postgres@sha256:abc", "../postgres", "library//postgres"} {
		in, f := imageAcquireFixture()
		in.Image.Repository = repository
		if _, err := AcquireGatewayImageWithClient(context.Background(), in, f); err == nil || f.creates != 0 {
			t.Fatalf("mutable repository accepted: %q", repository)
		}
	}
}
