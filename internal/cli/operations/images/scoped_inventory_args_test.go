package imageops

import (
	"reflect"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Explicitly-scoped image list syntax is owned by image operations.
func TestScopedImageInventoryArguments(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--force-local", "--project", "aginctus", "image", "list", "--format", "json"}
	if got := ScopedImageListArgs(scope); !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped list flags = %q; want %q", got, want)
	}
	for _, args := range [][]string{
		{"list", "--project", "aginctus"},
		{"ls", "--project=aginctus"},
	} {
		got, err := ParseScopedImageListArgs(args)
		if err != nil || got != "aginctus" {
			t.Errorf("args=%q project=%q err=%v", args, got, err)
		}
	}
	for _, args := range [][]string{
		nil, {"list"}, {"list", "--project"}, {"list", "--project="},
		{"list", "--project", "aginctus", "--force"},
		{"list", "--force-local"}, {"list", "--project", "default", "extra"},
	} {
		if _, err := ParseScopedImageListArgs(args); err == nil {
			t.Errorf("accepted malformed list args: %q", args)
		}
	}
}
