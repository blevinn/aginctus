package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

const auditOwnedProject = `[{"name":"aginctus","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`

func auditImageJSON(aliases []string, properties map[string]string) []byte {
	type imgAlias struct {
		Name string `json:"name"`
	}
	type image struct {
		Fingerprint string            `json:"fingerprint"`
		Aliases     []imgAlias        `json:"aliases"`
		Properties  map[string]string `json:"properties"`
	}
	var names []imgAlias
	for _, name := range aliases {
		names = append(names, imgAlias{Name: name})
	}
	raw, _ := json.Marshal([]image{{
		Fingerprint: strings.Repeat("a", 64),
		Aliases:     names,
		Properties:  properties,
	}})
	return raw
}

func TestImageOwnerClaimClassification(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	claim := expectedImageOwnerClaim(scope, "test-image", "system")
	copied := func() map[string]string {
		v := make(map[string]string, len(claim))
		for k, x := range claim {
			v[k] = x
		}
		return v
	}
	foreign := copied()
	foreign["user.aginctus.owner"] = "user"
	incomplete := copied()
	delete(incomplete, "user.aginctus.alias")
	wrongProject := copied()
	wrongProject["user.aginctus.deployment"] = "other"
	unmarked := map[string]string{"release": "26.05"}
	cases := []struct {
		name       string
		payload    []byte
		wantStatus string
	}{
		{"complete claim", auditImageJSON([]string{"test-image"}, copied()), "claimed"},
		{"unmarked image", auditImageJSON([]string{"test-image"}, unmarked), "unmarked"},
		{"incomplete claim", auditImageJSON([]string{"test-image"}, incomplete), "foreign-or-incomplete"},
		{"other owner", auditImageJSON([]string{"test-image"}, foreign), "foreign-or-incomplete"},
		{"other project", auditImageJSON([]string{"test-image"}, wrongProject), "foreign-or-incomplete"},
		{"shared fingerprint", auditImageJSON([]string{"test-image", "other-alias"}, copied()), "shared"},
		{"missing alias", auditImageJSON([]string{"other-alias"}, copied()), "absent"},
		{"no image", []byte("[]"), "absent"},
		{"duplicate same image", auditImageJSON([]string{"test-image", "test-image"}, copied()), "ambiguous"},
		{"duplicate across images", []byte("[" +
			strings.Trim(string(auditImageJSON([]string{"test-image"}, copied())), "[]") + "," +
			strings.Trim(string(auditImageJSON([]string{"test-image"}, copied())), "[]") + "]"), "ambiguous"},
		{"malformed fingerprint", []byte(`[{"fingerprint":"not-a-hash","aliases":[{"name":"test-image"}],"properties":{"user.aginctus.managed":"true"}}]`), "unverified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observed []observedClaimedImage
			if err := json.Unmarshal(tc.payload, &observed); err != nil {
				t.Fatalf("invalid test fixture: %v", err)
			}
			if got := classifyImageOwnerClaim(observed, scope, "test-image", "system"); got != tc.wantStatus {
				t.Fatalf("claim = %q, want %q", got, tc.wantStatus)
			}
		})
	}
}

func TestImageOwnerAuditRejectsInvalidArgumentsWithoutIncus(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	oldProjects, oldImages, oldLegacy := inspectLocalProjects, inspectScopedImages, runIncusImage
	t.Cleanup(func() { inspectLocalProjects, inspectScopedImages, runIncusImage = oldProjects, oldImages, oldLegacy })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		t.Fatal("project enumeration called for invalid args")
		return nil, nil
	}
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		t.Fatal("image inventory called for invalid args")
		return nil, nil
	}
	runIncusImage = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("ambient image runner invoked for ownership audit")
		return nil, nil
	}
	for _, args := range [][]string{
		{"owner-audit"}, {"owner-audit", "--project", "aginctus", "--owner", "system"},
		{"owner-audit", "--project", "default", "--owner", "system", "test-image"},
		{"owner-audit", "--project", "other", "--owner", "system", "test-image"},
		{"owner-audit", "--project", "aginctus", "--owner", "unknown", "test-image"},
		{"owner-audit", "--project", "aginctus", "--owner", "system", "../escape"},
		{"owner-audit", "--project", "aginctus", "--owner", "system", "test-image", "--force"},
		{"owner-audit", "--project=aginctus", "--owner=system", "test-image"},
	} {
		var out, errOut bytes.Buffer
		if code := runConfiguredImages(context.Background(), args, &out, &errOut, cfg); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestImageOwnerAuditScopedReadAndRedaction(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil {
		t.Fatal(err)
	}
	claim := expectedImageOwnerClaim(scope, "test-image", "system")
	tests := []struct {
		name, projects  string
		projectErr      error
		imageOutput     []byte
		imageErr        error
		expectedStatus  string
		wantCode, reads int
	}{
		{name: "claimed", projects: auditOwnedProject, imageOutput: auditImageJSON([]string{"test-image"}, claim), expectedStatus: "claimed", wantCode: 0, reads: 1},
		{name: "unmarked", projects: auditOwnedProject, imageOutput: auditImageJSON([]string{"test-image"}, nil), expectedStatus: "unmarked", wantCode: 1, reads: 1},
		{name: "absent", projects: auditOwnedProject, imageOutput: []byte("[]"), expectedStatus: "absent", wantCode: 1, reads: 1},
		{name: "no project", projects: "[]", wantCode: 1},
		{name: "foreign project", projects: strings.Replace(auditOwnedProject, "deployment-project", "other", 1), wantCode: 1},
		{name: "project denied", projectErr: errors.New("secret-token"), wantCode: 1},
		{name: "image denied", projects: auditOwnedProject, imageErr: errors.New("secret-token"), wantCode: 1, reads: 1},
		{name: "malformed inventory", projects: auditOwnedProject, imageOutput: []byte("{"), wantCode: 1, reads: 1},
		{name: "null inventory", projects: auditOwnedProject, imageOutput: []byte("null"), wantCode: 1, reads: 1},
		{name: "oversized inventory", projects: auditOwnedProject, imageOutput: bytes.Repeat([]byte("x"), maxProjectListBytes+1), wantCode: 1, reads: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldProjects, oldImages, oldLegacy := inspectLocalProjects, inspectScopedImages, runIncusImage
			t.Cleanup(func() {
				inspectLocalProjects, inspectScopedImages, runIncusImage = oldProjects, oldImages, oldLegacy
			})
			inspectLocalProjects = func(context.Context) ([]byte, error) {
				return []byte(tc.projects), tc.projectErr
			}
			reads := 0
			inspectScopedImages = func(_ context.Context, selected incusscope.ProjectScope) ([]byte, error) {
				reads++
				if selected.Name != scope.Name {
					t.Fatalf("wrong scope %q", selected.Name)
				}
				return tc.imageOutput, tc.imageErr
			}
			runIncusImage = func(context.Context, ...string) ([]byte, error) {
				t.Fatal("used ambient project")
				return nil, nil
			}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{
				"images", "owner-audit", "--project", "aginctus",
				"--owner", "system", "test-image",
			}, &out, &errOut, nil, &fakeConfigLoader{cfg: cfg})
			if code != tc.wantCode || reads != tc.reads {
				t.Fatalf("code=%d reads=%d stdout=%q stderr=%q, expected code %d reads %d", code, reads, out.String(), errOut.String(), tc.wantCode, tc.reads)
			}
			if strings.Contains(out.String()+errOut.String(), "secret-token") {
				t.Fatal("subprocess error detail leaked")
			}
			if tc.expectedStatus != "" {
				var got imageOwnerAuditReport
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatalf("invalid JSON report: %q: %v", out.String(), err)
				}
				if got.Status != tc.expectedStatus || got.Project != "aginctus" ||
					got.Owner != "system" || got.Alias != "test-image" {
					t.Fatalf("unexpected report: %+v", got)
				}
			} else if out.Len() != 0 {
				t.Fatalf("unverified inventory printed a report: %q", out.String())
			}
		})
	}
}

func TestImageOwnerAuditOtherLifecycleOwnerNotClaimed(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	properties := expectedImageOwnerClaim(scope, "test-image", "system")
	var images []observedClaimedImage
	if err := json.Unmarshal(auditImageJSON([]string{"test-image"}, properties), &images); err != nil {
		t.Fatal(err)
	}
	if status := classifyImageOwnerClaim(images, scope, "test-image", "user"); status != "foreign-or-incomplete" {
		t.Fatalf("user audit may not claim a system-owned image: %q", status)
	}
}
