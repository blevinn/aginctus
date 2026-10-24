package incus

import (
	"reflect"
	"strings"
	"testing"
)

func TestProjectScopeValidation(t *testing.T) {
	for _, name := range []string{"", "default", "../other", "other/name", "my project", "-leading", strings.Repeat("a", 101)} {
		if _, err := NewProjectScope(name); err == nil {
			t.Errorf("accepted forbidden Incus project %q", name)
		}
	}
	for _, name := range []string{"aginctus", "aginctus-dev", "a.b_c", "0"} {
		scope, err := NewProjectScope(name)
		if err != nil || scope.Name != name {
			t.Errorf("valid %q: scope=%+v err=%v", name, scope, err)
		}
	}
	if DefaultAginctusProject != "aginctus" {
		t.Fatalf("unexpected desired deployment project: %q", DefaultAginctusProject)
	}
}

func TestProjectScopeRequiredPolicy(t *testing.T) {
	scope, err := NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"user.aginctus.managed": "true",
		"user.aginctus.resource": "deployment-project",
		"user.aginctus.deployment": "aginctus",
		"features.images": "true",
		"features.profiles": "true",
		"features.storage.volumes": "true",
		"features.networks": "false",
	}
	if got := scope.DesiredConfig(); !reflect.DeepEqual(got, want) {
		t.Errorf("desired policy: got %v; want %v", got, want)
	}
	original := scope.DesiredConfig()
	original["features.networks"] = "true"
	if scope.DesiredConfig()["features.networks"] != "false" {
		t.Fatal("desired policy leaked mutable shared map")
	}
}

func TestAssessProjectRejectsForeignAndDrift(t *testing.T) {
	scope, err := NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	good := scope.DesiredConfig()
	if got := scope.AssessProject("aginctus", good); got != ProjectCompatible {
		t.Fatalf("owned project: %s", got)
	}
	for _, pair := range [][2]string{
		{"user.aginctus.managed", "false"},
		{"user.aginctus.resource", "compose-project"},
		{"user.aginctus.deployment", "someone-else"},
	} {
		found := scope.DesiredConfig()
		found[pair[0]] = pair[1]
		if got := scope.AssessProject("aginctus", found); got != ProjectForeign {
			t.Errorf("foreign marker %s: got %s", pair[0], got)
		}
	}
	if got := scope.AssessProject("foreign", good); got != ProjectForeign {
		t.Errorf("wrong project identity: %s", got)
	}
	for _, pair := range [][2]string{
		{"features.images", "false"},
		{"features.profiles", "false"},
		{"features.storage.volumes", "false"},
		{"features.networks", "true"},
	} {
		found := scope.DesiredConfig()
		found[pair[0]] = pair[1]
		if got := scope.AssessProject("aginctus", found); got != ProjectDrift {
			t.Errorf("policy drift %s: got %s", pair[0], got)
		}
	}
	incomplete := scope.DesiredConfig()
	delete(incomplete, "features.networks")
	if got := scope.AssessProject("aginctus", incomplete); got != ProjectDrift {
		t.Errorf("missing expected project feature: %s", got)
	}
	// Extra non-Aginctus metadata belongs to Incus/the operator and does
	// not by itself authorize the project's ownership or count as drift.
	good["description.unrelated"] = "untouched"
	if got := scope.AssessProject("aginctus", good); got != ProjectCompatible {
		t.Errorf("unrelated project metadata: %s", got)
	}
}
