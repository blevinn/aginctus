package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraRootGlobalOptionsPreserveAliasesAndOrder(t *testing.T) {
	loader := &fakeConfigLoader{}
	var out, errors bytes.Buffer
	args := []string{"--config=a.b=first", "--configure", "a.b=second", "config", "get", "a.b"}
	if code := Run(context.Background(), args, &out, &errors, nil, loader); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errors.String())
	}
	if len(loader.options.Overrides) != 2 ||
		loader.options.Overrides[0] != "a.b=first" ||
		loader.options.Overrides[1] != "a.b=second" {
		t.Fatalf("global overrides were not preserved: %#v", loader.options.Overrides)
	}
}

func TestCobraRootHelpAndErrorsDoNotLoadConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"help", "gateway"},
		{"gateway", "--help"},
		{"version"},
		{"--version"},
		{"unknown"},
		{"--configuration-file=","doctor"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errors bytes.Buffer
			loader := &fakeConfigLoader{err: context.Canceled}
			_ = Run(context.Background(), args, &out, &errors, nil, loader)
			if strings.Contains(errors.String(), "configuration:") {
				t.Fatalf("configuration read during help/version/syntax: %q", errors.String())
			}
		})
	}
}

func TestCobraRootNormalizesLegacyWithGlobalFlags(t *testing.T) {
	got := normalizeLegacyCLIArgs([]string{"--config", "a.b=c", "gateway", "model", "coding", "show"})
	want := []string{"--config", "a.b=c", "gateway", "model", "show", "coding"}
	if strings.Join(got, "/") != strings.Join(want, "/") {
		t.Fatalf("legacy args %q; want %q", got, want)
	}
}
