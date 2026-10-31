package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraBasicRejectsSyntaxBeforeConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"config", "missing"},
		{"config", "get"},
		{"config", "get", "name", "extra"},
		{"config", "show", "extra"},
		{"doctor", "extra"},
		{"doctor", "--unsupported"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			loader := &fakeConfigLoader{err: context.Canceled}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), args, &out, &errOut, nil, loader)
			if code != 2 || out.Len() != 0 || errOut.Len() == 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if len(loader.options.Overrides) != 0 {
				t.Fatalf("unexpected config options: %#v", loader.options)
			}
			if strings.Contains(errOut.String(), "configuration:") {
				t.Fatalf("loaded config before syntax validation: %q", errOut.String())
			}
		})
	}
}

func TestCobraBasicDoesNotRetainFlagStateAcrossRuns(t *testing.T) {
	for _, args := range [][]string{{"doctor", "--fix"}, {"doctor"}} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, &fakeIncusClient{version: "7.0.1"}, &fakeConfigLoader{})
		if code != 0 || errOut.Len() != 0 {
			t.Fatalf("args=%q code=%d stderr=%q", args, code, errOut.String())
		}
	}
}

func TestCobraConfigLoadsOnlyAfterValidArguments(t *testing.T) {
	loader := &fakeConfigLoader{}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"--configure=workloads.dev.runtime=opencode", "config", "get", "workloads.dev.runtime"},
		&out, &errOut, nil, loader)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "opencode") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if len(loader.options.Overrides) != 1 {
		t.Fatalf("missing override: %#v", loader.options)
	}
}
