package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func scopedImportFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	data := []byte("immutable test image")
	for _, subpath := range []string{"metadata/data.tar.xz", "rootfs/data.squashfs"} {
		path := filepath.Join(dir, subpath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(append(append([]byte(nil), data...), data...))
	return dir, fmt.Sprintf("%x", sum)
}

func TestScopedImageImportArgs(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--force-local", "--project", "aginctus", "image", "import",
		"/store/metadata.tar.xz", "/store/rootfs.squashfs", "--alias", "my-image",
	}
	got := scopedImageImportArgs(scope, "/store/metadata.tar.xz", "/store/rootfs.squashfs", "my-image")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("image import args=%q, want=%q", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "--reuse") {
		t.Fatal("create-only import cannot reuse an existing alias")
	}
	for _, args := range [][]string{
		{"import", "--project", "aginctus", "my-image", "/store/build", "--confirm-create"},
		{"import", "--project=aginctus", "my-image", "/store/build", "--confirm-create"},
	} {
		project, alias, directory, err := parseScopedImageImportArgs(args)
		if err != nil || project != "aginctus" || alias != "my-image" || directory != "/store/build" {
			t.Fatalf("valid import syntax %q yielded %q %q %q err=%v", args, project, alias, directory, err)
		}
	}
	for _, args := range [][]string{
		nil, {"import"}, {"import", "--project", "aginctus", "my-image", "/store/build"},
		{"import", "--project", "default", "my-image", "/store/build", "--confirm-create"},
		{"import", "--project=../other", "my-image", "/store/build", "--confirm-create"},
		{"import", "--project", "aginctus", "../evil", "/store/build", "--confirm-create"},
		{"import", "--project", "aginctus", "my-image", "", "--confirm-create"},
		{"import", "--project", "aginctus", "my-image", "/store/build", "--reuse"},
		{"import", "--project", "aginctus", "my-image", "/store/build", "--confirm-create", "--force"},
	} {
		if _, _, _, err := parseScopedImageImportArgs(args); err == nil {
			t.Errorf("accepted unsafe import flags %q", args)
		}
	}
}

func TestScopedImageImportRefusesConflictsAndUncertainState(t *testing.T) {
	dir, fingerprint := scopedImportFixture(t)
	owned := `[{"name":"aginctus","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`
	foreign := strings.Replace(owned, "deployment-project", "foreign-project", 1)
	drift := strings.Replace(owned, `"features.images":"true"`, `"features.images":"false"`, 1)
	absent := `[{"fingerprint":"other-image","aliases":[{"name":"other-alias"}]}]`
	conflict := `[{"fingerprint":"foreign-hash","aliases":[{"name":"my-image"}]}]`
	created := fmt.Sprintf(`[{"fingerprint":"%s","aliases":[{"name":"my-image"}]}]`, fingerprint)
	mismatch := `[{"fingerprint":"unexpected-fingerprint","aliases":[{"name":"my-image"}]}]`
	const sensitive = "sensitive external subprocess text"
	cases := []struct {
		name           string
		projectReads   []string
		images         []string
		projectErr     error
		imageErr       error
		importErr      error
		wantCode       int
		wantImports    int
		wantProjects   int
		wantImageReads int
	}{
		{"created and verified", []string{owned, owned, owned}, []string{absent, absent, created}, nil, nil, nil, 0, 1, 3, 3},
		{"foreign target", []string{foreign}, nil, nil, nil, nil, 1, 0, 1, 0},
		{"policy drift", []string{drift}, nil, nil, nil, nil, 1, 0, 1, 0},
		{"not visible", []string{"[]"}, nil, nil, nil, nil, 1, 0, 1, 0},
		{"project read denied", []string{owned}, nil, errors.New(sensitive), nil, nil, 1, 0, 1, 0},
		{"alias exists at first read", []string{owned}, []string{conflict}, nil, nil, nil, 1, 0, 1, 1},
		{"project foreign at second check", []string{owned, foreign}, []string{absent}, nil, nil, nil, 1, 0, 2, 1},
		{"alias appears at second check", []string{owned, owned}, []string{absent, conflict}, nil, nil, nil, 1, 0, 2, 2},
		{"second inventory fails", []string{owned, owned}, []string{absent, "null"}, nil, nil, nil, 1, 0, 2, 2},
		{"server refuses collision", []string{owned, owned}, []string{absent, absent}, nil, nil, errors.New(sensitive), 1, 1, 2, 2},
		{"alias absent after import", []string{owned, owned, owned}, []string{absent, absent, absent}, nil, nil, nil, 1, 1, 3, 3},
		{"alias differs after import", []string{owned, owned, owned}, []string{absent, absent, mismatch}, nil, nil, nil, 1, 1, 3, 3},
		{"post-import project drift", []string{owned, owned, drift}, []string{absent, absent}, nil, nil, nil, 1, 1, 3, 2},
		{"malformed first inventory", []string{owned}, []string{"null"}, nil, nil, nil, 1, 0, 1, 1},
		{"image read denied", []string{owned}, []string{absent}, nil, errors.New(sensitive), nil, 1, 0, 1, 1},
		{"duplicate alias rejected", []string{owned}, []string{`[{"fingerprint":"a","aliases":[{"name":"my-image"}]},{"fingerprint":"b","aliases":[{"name":"my-image"}]}]`}, nil, nil, nil, 1, 0, 1, 1},
	}
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeProjects, beforeImages, beforeImport, beforeLegacy := inspectLocalProjects, inspectScopedImages, importScopedImage, runIncusImage
			t.Cleanup(func() {
				inspectLocalProjects, inspectScopedImages, importScopedImage, runIncusImage = beforeProjects, beforeImages, beforeImport, beforeLegacy
			})
			projectReads, imageReads, writes := 0, 0, 0
			inspectLocalProjects = func(context.Context) ([]byte, error) {
				projectReads++
				if projectReads > len(tc.projectReads) {
					t.Fatalf("unexpected project ownership reinspection #%d", projectReads)
				}
				return []byte(tc.projectReads[projectReads-1]), tc.projectErr
			}
			inspectScopedImages = func(_ context.Context, scope incusscope.ProjectScope) ([]byte, error) {
				imageReads++
				if scope.Name != "aginctus" || imageReads > len(tc.images) {
					t.Fatalf("unexpected scoped images query %s #%d", scope.Name, imageReads)
				}
				return []byte(tc.images[imageReads-1]), tc.imageErr
			}
			importScopedImage = func(_ context.Context, scope incusscope.ProjectScope, metadata, rootfs, alias string) error {
				writes++
				if scope.Name != "aginctus" || alias != "my-image" ||
					metadata != filepath.Join(dir, "metadata", "data.tar.xz") ||
					rootfs != filepath.Join(dir, "rootfs", "data.squashfs") {
					t.Fatalf("wrong import scope or artifacts: %+v %s %s %s", scope, metadata, rootfs, alias)
				}
				return tc.importErr
			}
			runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
				t.Fatalf("legacy image runner used: %v", args)
				return nil, nil
			}
			var stdout, stderr bytes.Buffer
			code := runScopedImageImport(context.Background(), []string{"import", "--project", "aginctus", "my-image", dir, "--confirm-create"}, &stdout, &stderr, cfg)
			if code != tc.wantCode || writes != tc.wantImports ||
				projectReads != tc.wantProjects || imageReads != tc.wantImageReads {
				t.Fatalf("code=%d, imports=%d, projectReads=%d, imageReads=%d, expected %d/%d/%d/%d, stderr=%q",
					code, writes, projectReads, imageReads, tc.wantCode, tc.wantImports, tc.wantProjects, tc.wantImageReads, stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), sensitive) {
				t.Fatal("raw subprocess details leaked to terminal")
			}
			if code == 0 && stdout.String() != "image \"my-image\": created in project \"aginctus\"\n" {
				t.Fatalf("unexpected success result: %q", stdout.String())
			}
			if code != 0 && stdout.Len() != 0 {
				t.Fatalf("failed import printed success: %q", stdout.String())
			}
		})
	}
}

func TestScopedImageImportInvalidArgsNeverContactIncus(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	beforeProjects, beforeImages, beforeImport := inspectLocalProjects, inspectScopedImages, importScopedImage
	t.Cleanup(func() { inspectLocalProjects, inspectScopedImages, importScopedImage = beforeProjects, beforeImages, beforeImport })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		t.Fatal("queried Incus for invalid args")
		return nil, nil
	}
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		t.Fatal("listed images for invalid args")
		return nil, nil
	}
	importScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string) error {
		t.Fatal("imported for invalid args")
		return nil
	}
	for _, args := range [][]string{
		{"import"}, {"import", "--project", "aginctus", "alias", "/x"},
		{"import", "--project", "default", "alias", "/x", "--confirm-create"},
		{"import", "--project", "other", "alias", "/x", "--confirm-create"},
		{"import", "--project=aginctus", "alias", "/x", "--reuse"},
	} {
		var out, errOut bytes.Buffer
		if code := runConfiguredImages(context.Background(), args, &out, &errOut, cfg); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%q, code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestScopedImageImportTopLevelDispatch(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir, fingerprint := scopedImportFixture(t)
	oldProjects, oldImages, oldImport, oldLegacy := inspectLocalProjects, inspectScopedImages, importScopedImage, runIncusImage
	t.Cleanup(func() { inspectLocalProjects, inspectScopedImages, importScopedImage, runIncusImage = oldProjects, oldImages, oldImport, oldLegacy })
	owned := `[{"name":"aginctus","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`
	inspectLocalProjects = func(context.Context) ([]byte, error) { return []byte(owned), nil }
	images := 0
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		images++
		if images == 3 {
			return []byte(fmt.Sprintf(`[{"fingerprint":"%s","aliases":[{"name":"my-image"}]}]`, fingerprint)), nil
		}
		return []byte("[]"), nil
	}
	importScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string) error { return nil }
	runIncusImage = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("ambient-project mutation attempted")
		return nil, nil
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"images", "import", "--project=aginctus", "my-image", dir, "--confirm-create"}, &out, &errOut, nil, &fakeConfigLoader{cfg: cfg}); code != 0 {
		t.Fatalf("top-level import returned %d: %q", code, errOut.String())
	}
	if images != 3 || !strings.Contains(out.String(), "created in project") {
		t.Fatalf("missing post-import verification: reads=%d stdout=%q", images, out.String())
	}
}
