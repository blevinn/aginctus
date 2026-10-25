package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func ownedImageFixture(fingerprint string, aliases []string, props map[string]string) []byte {
	type item struct {
		Name string `json:"name"`
	}
	type record struct {
		Fingerprint string            `json:"fingerprint"`
		Aliases     []item            `json:"aliases"`
		Properties  map[string]string `json:"properties"`
	}
	names := make([]item, 0, len(aliases))
	for _, alias := range aliases {
		names = append(names, item{Name: alias})
	}
	raw, _ := json.Marshal([]record{{Fingerprint: fingerprint, Aliases: names, Properties: props}})
	return raw
}

func TestScopedOwnedImageImportArguments(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	got := scopedOwnedImageImportArgs(scope, "/tmp/m.tar.xz", "/tmp/r.squashfs", "my-image", "system")
	want := []string{
		"--force-local", "--project", "aginctus", "image", "import",
		"/tmp/m.tar.xz", "/tmp/r.squashfs",
		"user.aginctus.alias=my-image",
		"user.aginctus.deployment=aginctus",
		"user.aginctus.managed=true",
		"user.aginctus.owner=system",
		"user.aginctus.resource=image",
		"--alias", "my-image",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("import flags = %q; want %q", got, want)
	}
	for _, arg := range got {
		if arg == "--reuse" || arg == "--public" || arg == "--all-projects" {
			t.Fatalf("unsafe import argument: %q", arg)
		}
	}
	if !reflect.DeepEqual(scopedOwnedImageImportArgs(scope, "/tmp/m.tar.xz", "/tmp/r.squashfs", "my-image", "user")[7:12],
		[]string{"user.aginctus.alias=my-image", "user.aginctus.deployment=aginctus", "user.aginctus.managed=true", "user.aginctus.owner=user", "user.aginctus.resource=image"}) {
		t.Fatal("user claim not correctly encoded")
	}
	if project, owner, alias, dir, err := parseScopedOwnedImageImportArgs([]string{
		"import-owned", "--project", "aginctus", "--owner", "system", "my-image", "/tmp/artifacts", "--confirm-create",
	}); err != nil || project != "aginctus" || owner != "system" || alias != "my-image" || dir != "/tmp/artifacts" {
		t.Fatalf("parse owned import: %q %q %q %q err=%v", project, owner, alias, dir, err)
	}
	for _, args := range [][]string{
		nil, {"import-owned"}, {"import-owned", "--project", "aginctus", "--owner", "system", "alias", "/tmp/a"},
		{"import-owned", "--project", "default", "--owner", "system", "alias", "/tmp/a", "--confirm-create"},
		{"import-owned", "--project", "../escape", "--owner", "system", "alias", "/tmp/a", "--confirm-create"},
		{"import-owned", "--project", "aginctus", "--owner", "root", "alias", "/tmp/a", "--confirm-create"},
		{"import-owned", "--project", "aginctus", "--owner", "system", "../bad", "/tmp/a", "--confirm-create"},
		{"import-owned", "--project", "aginctus", "--owner", "system", "alias", "", "--confirm-create"},
		{"import-owned", "--project", "aginctus", "--owner", "user", "alias", "/tmp/a", "--reuse"},
		{"import-owned", "--project", "aginctus", "--owner", "user", "alias", "/tmp/a", "--confirm-create", "--force"},
	} {
		if _, _, _, _, err := parseScopedOwnedImageImportArgs(args); err == nil {
			t.Errorf("accepted unsafe input: %q", args)
		}
	}
}

func TestScopedOwnedImportSafetyAndPostconditions(t *testing.T) {
	dir, fingerprint := scopedImportFixture(t)
	scope, _ := incusscope.NewProjectScope("aginctus")
	claim := expectedImageOwnerClaim(scope, "my-image", "system")
	owned := auditOwnedProject
	foreign := strings.Replace(owned, "deployment-project", "other", 1)
	drift := strings.Replace(owned, `"features.images":"true"`, `"features.images":"false"`, 1)
	unrelated := ownedImageFixture(strings.Repeat("b", 64), []string{"other"}, nil)
	conflictingAlias := ownedImageFixture(strings.Repeat("b", 64), []string{"my-image"}, nil)
	sameFingerprint := ownedImageFixture(fingerprint, []string{"other"}, nil)
	verified := ownedImageFixture(fingerprint, []string{"my-image"}, claim)
	wrongOwner := expectedImageOwnerClaim(scope, "my-image", "user")
	wrongClaim := ownedImageFixture(fingerprint, []string{"my-image"}, wrongOwner)
	shared := ownedImageFixture(fingerprint, []string{"my-image", "other"}, claim)
	wrongFingerprint := ownedImageFixture(strings.Repeat("c", 64), []string{"my-image"}, claim)

	tests := []struct {
		name             string
		projects         []string
		inventories      [][]byte
		importError      error
		wantCode         int
		wantWrites       int
		wantProjectReads int
		wantImageReads   int
	}{
		{name: "creates and verifies", projects: []string{owned, owned, owned}, inventories: [][]byte{unrelated, unrelated, verified}, wantProjectReads: 3, wantImageReads: 3, wantWrites: 1},
		{name: "foreign project", projects: []string{foreign}, wantCode: 1, wantProjectReads: 1},
		{name: "drifted project", projects: []string{drift}, wantCode: 1, wantProjectReads: 1},
		{name: "not visible", projects: []string{"[]"}, wantCode: 1, wantProjectReads: 1},
		{name: "invalid initial inventory", projects: []string{owned}, inventories: [][]byte{[]byte("null")}, wantCode: 1, wantProjectReads: 1, wantImageReads: 1},
		{name: "invalid image fingerprint", projects: []string{owned}, inventories: [][]byte{[]byte(`[{"fingerprint":"bad","aliases":[]}]`)}, wantCode: 1, wantProjectReads: 1, wantImageReads: 1},
		{name: "alias preexists", projects: []string{owned}, inventories: [][]byte{conflictingAlias}, wantCode: 1, wantProjectReads: 1, wantImageReads: 1},
		{name: "existing content fingerprint", projects: []string{owned}, inventories: [][]byte{sameFingerprint}, wantCode: 1, wantProjectReads: 1, wantImageReads: 1},
		{name: "prewrite project drift", projects: []string{owned, drift}, inventories: [][]byte{unrelated}, wantCode: 1, wantProjectReads: 2, wantImageReads: 1},
		{name: "prewrite alias conflict", projects: []string{owned, owned}, inventories: [][]byte{unrelated, conflictingAlias}, wantCode: 1, wantProjectReads: 2, wantImageReads: 2},
		{name: "prewrite fingerprint race", projects: []string{owned, owned}, inventories: [][]byte{unrelated, sameFingerprint}, wantCode: 1, wantProjectReads: 2, wantImageReads: 2},
		{name: "prewrite malformed inventory", projects: []string{owned, owned}, inventories: [][]byte{unrelated, []byte("{")}, wantCode: 1, wantProjectReads: 2, wantImageReads: 2},
		{name: "server rejects racing create", projects: []string{owned, owned}, inventories: [][]byte{unrelated, unrelated}, importError: errors.New("private daemon output"), wantCode: 1, wantProjectReads: 2, wantImageReads: 2, wantWrites: 1},
		{name: "postwrite project drift", projects: []string{owned, owned, drift}, inventories: [][]byte{unrelated, unrelated}, wantCode: 1, wantProjectReads: 3, wantImageReads: 2, wantWrites: 1},
		{name: "postwrite absent image", projects: []string{owned, owned, owned}, inventories: [][]byte{unrelated, unrelated, unrelated}, wantCode: 1, wantProjectReads: 3, wantImageReads: 3, wantWrites: 1},
		{name: "postwrite wrong owner", projects: []string{owned, owned, owned}, inventories: [][]byte{unrelated, unrelated, wrongClaim}, wantCode: 1, wantProjectReads: 3, wantImageReads: 3, wantWrites: 1},
		{name: "postwrite shared image", projects: []string{owned, owned, owned}, inventories: [][]byte{unrelated, unrelated, shared}, wantCode: 1, wantProjectReads: 3, wantImageReads: 3, wantWrites: 1},
		{name: "postwrite wrong fingerprint", projects: []string{owned, owned, owned}, inventories: [][]byte{unrelated, unrelated, wrongFingerprint}, wantCode: 1, wantProjectReads: 3, wantImageReads: 3, wantWrites: 1},
	}
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldProjects, oldImages, oldImport, oldLegacy := inspectLocalProjects, inspectScopedImages, importOwnedScopedImage, runIncusImage
			t.Cleanup(func() {
				inspectLocalProjects, inspectScopedImages, importOwnedScopedImage, runIncusImage = oldProjects, oldImages, oldImport, oldLegacy
			})
			projectReads, imageReads, writes := 0, 0, 0
			inspectLocalProjects = func(context.Context) ([]byte, error) {
				projectReads++
				if projectReads > len(tc.projects) {
					t.Fatal("unexpected project read")
				}
				return []byte(tc.projects[projectReads-1]), nil
			}
			inspectScopedImages = func(_ context.Context, selected incusscope.ProjectScope) ([]byte, error) {
				imageReads++
				if selected.Name != scope.Name || imageReads > len(tc.inventories) {
					t.Fatalf("unexpected inventory read %d for %s", imageReads, selected.Name)
				}
				return tc.inventories[imageReads-1], nil
			}
			importOwnedScopedImage = func(_ context.Context, selected incusscope.ProjectScope, metadata, rootfs, alias, owner string) error {
				writes++
				if selected.Name != scope.Name || alias != "my-image" || owner != "system" ||
					metadata != filepath.Join(dir, "metadata", "data.tar.xz") ||
					rootfs != filepath.Join(dir, "rootfs", "data.squashfs") {
					t.Fatalf("incorrect scoped write arguments: %+v %q %q %q %q", selected, metadata, rootfs, alias, owner)
				}
				return tc.importError
			}
			runIncusImage = func(context.Context, ...string) ([]byte, error) {
				t.Fatal("legacy ambient-project image runner used")
				return nil, nil
			}
			var out, errOut bytes.Buffer
			code := runConfiguredImages(context.Background(), []string{
				"import-owned", "--project", "aginctus", "--owner", "system", "my-image", dir, "--confirm-create",
			}, &out, &errOut, cfg)
			if code != tc.wantCode || writes != tc.wantWrites ||
				projectReads != tc.wantProjectReads || imageReads != tc.wantImageReads {
				t.Fatalf("code=%d writes=%d projects=%d images=%d expected %d/%d/%d/%d stderr=%q",
					code, writes, projectReads, imageReads, tc.wantCode, tc.wantWrites, tc.wantProjectReads, tc.wantImageReads, errOut.String())
			}
			if code != 0 && out.Len() > 0 {
				t.Fatalf("failure printed success: %q", out.String())
			}
			if code == 0 && !strings.Contains(out.String(), "created with \"system\" claim") {
				t.Fatalf("missing creation confirmation: %q", out.String())
			}
			if strings.Contains(out.String()+errOut.String(), "private daemon output") {
				t.Fatal("raw external error leaked")
			}
		})
	}
}

func TestScopedOwnedImportCLIRejectsWrongProjectWithoutIncus(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	oldProjects, oldImages, oldImport := inspectLocalProjects, inspectScopedImages, importOwnedScopedImage
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedImages, importOwnedScopedImage = oldProjects, oldImages, oldImport
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		t.Fatal("queried Incus for invalid input")
		return nil, nil
	}
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		t.Fatal("inspected images for invalid input")
		return nil, nil
	}
	importOwnedScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string, string) error {
		t.Fatal("imported images for invalid input")
		return nil
	}
	for _, args := range [][]string{
		{"images", "import-owned", "--project", "other", "--owner", "system", "img", "/data", "--confirm-create"},
		{"images", "import-owned", "--project", "default", "--owner", "system", "img", "/data", "--confirm-create"},
		{"images", "import-owned", "--project", "aginctus", "--owner", "root", "img", "/data", "--confirm-create"},
		{"images", "import-owned", "--project", "aginctus", "--owner", "system", "img", "/data"},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, nil, &fakeConfigLoader{cfg: cfg})
		if code != 2 || out.Len() != 0 {
			t.Fatalf("invalid input %q: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}
