package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestScopedImageInventoryArguments(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--force-local", "--project", "aginctus", "image", "list", "--format", "json"}
	if got := scopedImageListArgs(scope); !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped list flags = %q; want %q", got, want)
	}
	for _, args := range [][]string{
		{"list", "--project", "aginctus"},
		{"ls", "--project=aginctus"},
	} {
		got, err := parseScopedImageListArgs(args)
		if err != nil || got != "aginctus" {
			t.Errorf("args=%q project=%q err=%v", args, got, err)
		}
	}
	for _, args := range [][]string{
		nil, {"list"}, {"list", "--project"}, {"list", "--project="},
		{"list", "--project", "aginctus", "--force"},
		{"list", "--force-local"}, {"list", "--project", "default", "extra"},
	} {
		if _, err := parseScopedImageListArgs(args); err == nil {
			t.Errorf("accepted malformed list args: %q", args)
		}
	}
}

func TestScopedImageInventoryFailsClosed(t *testing.T) {
	configured, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := configured.IncusProjectScope()
	if err != nil {
		t.Fatal(err)
	}
	originalProjectRead, originalImageRead, originalLegacy := inspectLocalProjects, inspectScopedImages, runIncusImage
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedImages, runIncusImage = originalProjectRead, originalImageRead, originalLegacy
	})
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		t.Fatalf("ambient-project image runner called: %q", args)
		return nil, nil
	}
	const private = "secret-private-subprocess-output"
	owned := `[{"name":"aginctus","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`
	foreign := strings.Replace(owned, "deployment-project", "foreign-project", 1)
	drift := strings.Replace(owned, `"features.images":"true"`, `"features.images":"false"`, 1)
	type specimen struct {
		name       string
		projects   string
		projectErr error
		images     string
		imageErr   error
		code       int
		imageCalls int
	}
	tests := []specimen{
		{name: "compatible", projects: owned, images: `[{"fingerprint":"sha","aliases":[{"name":"owned-alias"}]}]`, code: 0, imageCalls: 1},
		{name: "compatible empty", projects: owned, images: "[]", code: 0, imageCalls: 1},
		{name: "foreign", projects: foreign, code: 1},
		{name: "policy drift", projects: drift, code: 1},
		{name: "not visible", projects: "[]", code: 1},
		{name: "invalid project response", projects: "null", code: 1},
		{name: "daemon denied", projectErr: errors.New(private), code: 1},
		{name: "image denied", projects: owned, imageErr: errors.New(private), code: 1, imageCalls: 1},
		{name: "invalid images", projects: owned, images: "null", code: 1, imageCalls: 1},
		{name: "malformed images", projects: owned, images: "{", code: 1, imageCalls: 1},
		{name: "oversized images", projects: owned, images: strings.Repeat("x", maxProjectListBytes+1), code: 1, imageCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectCalls, imageCalls := 0, 0
			inspectLocalProjects = func(context.Context) ([]byte, error) {
				projectCalls++
				return []byte(tc.projects), tc.projectErr
			}
			inspectScopedImages = func(_ context.Context, selected incusscope.ProjectScope) ([]byte, error) {
				imageCalls++
				if selected.Name != scope.Name {
					t.Fatalf("selected wrong project %q", selected.Name)
				}
				return []byte(tc.images), tc.imageErr
			}
			var stdout, stderr bytes.Buffer
			code := runConfiguredImages(context.Background(), []string{"list", "--project", scope.Name}, &stdout, &stderr, configured)
			if code != tc.code || projectCalls != 1 || imageCalls != tc.imageCalls {
				t.Fatalf("code=%d projectReads=%d imageReads=%d stdout=%q stderr=%q", code, projectCalls, imageCalls, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), private) {
				t.Fatal("leaked raw Incus command error")
			}
			if code == 0 && tc.name == "compatible" && stdout.String() != "owned-alias\tsha\n" {
				t.Fatalf("unexpected image output %q", stdout.String())
			}
			if code != 0 && stdout.Len() != 0 {
				t.Fatalf("partial image output leaked: %q", stdout.String())
			}
		})
	}
}

func TestScopedImageInventoryRejectsImplicitOrMismatchedProject(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	priorProject, priorImage, priorLegacy := inspectLocalProjects, inspectScopedImages, runIncusImage
	t.Cleanup(func() { inspectLocalProjects, inspectScopedImages, runIncusImage = priorProject, priorImage, priorLegacy })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		t.Fatal("project queried for invalid arguments")
		return nil, nil
	}
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		t.Fatal("image list issued for invalid arguments")
		return nil, nil
	}
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		t.Fatalf("legacy ambient-project path called: %q", args)
		return nil, nil
	}
	for _, args := range [][]string{
		{"list", "--project"}, {"list", "--project="},
		{"ls", "--project=default"}, {"list", "--project", "other"},
		{"list", "--project", "aginctus", "--extra"},
	} {
		var out, errOut bytes.Buffer
		if code := runConfiguredImages(context.Background(), args, &out, &errOut, cfg); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestScopedImageInventoryUsesCLIDispatch(t *testing.T) {
	previousProjects, previousImages, previousLegacy := inspectLocalProjects, inspectScopedImages, runIncusImage
	t.Cleanup(func() { inspectLocalProjects, inspectScopedImages, runIncusImage = previousProjects, previousImages, previousLegacy })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		return []byte(`[{"name":"aginctus","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`), nil
	}
	imageCalls := 0
	inspectScopedImages = func(_ context.Context, scope incusscope.ProjectScope) ([]byte, error) {
		imageCalls++
		if scope.Name != "aginctus" {
			t.Fatalf("wrong image read scope: %q", scope.Name)
		}
		return []byte(`[{"fingerprint":"hash","aliases":[{"name":"example"}]}]`), nil
	}
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		t.Fatalf("ambient image command reached: %q", args)
		return nil, nil
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"images", "list", "--project=aginctus"}, &stdout, &stderr, nil, &config.Loader{Environment: nil})
	if code != 0 || imageCalls != 1 || stdout.String() != "example\thash\n" {
		t.Fatalf("CLI returned %d, image reads %d, stdout=%q stderr=%q", code, imageCalls, stdout.String(), stderr.String())
	}
}

func TestScopedImageInventoryHonorsEffectiveConfigOverride(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{
		Overrides: []string{"incus.project=aginctus-lab"},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldProjects, oldImages, oldLegacy := inspectLocalProjects, inspectScopedImages, runIncusImage
	t.Cleanup(func() { inspectLocalProjects, inspectScopedImages, runIncusImage = oldProjects, oldImages, oldLegacy })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		return []byte(`[{"name":"aginctus-lab","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus-lab","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`), nil
	}
	inspectScopedImages = func(_ context.Context, selected incusscope.ProjectScope) ([]byte, error) {
		if selected.Name != "aginctus-lab" {
			t.Fatalf("wrong project %q", selected.Name)
		}
		return []byte("[]"), nil
	}
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		t.Fatalf("legacy ambient operation called: %q", args)
		return nil, nil
	}
	var out, errOut bytes.Buffer
	if code := runConfiguredImages(context.Background(), []string{"ls", "--project=aginctus-lab"}, &out, &errOut, cfg); code != 0 {
		t.Fatalf("declared alternate project rejected: %d %q", code, errOut.String())
	}
}
