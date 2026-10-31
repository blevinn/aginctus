package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraAggregateSyntaxHasNoConfigSideEffects(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "--invalid"},
		{"apply", "--render", "--from", "sample.yaml"},
		{"apply", "unexpected"},
		{"remove", "--force"},
		{"status", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			loader := &fakeConfigLoader{err: context.Canceled}
			var out, errors bytes.Buffer
			code := Run(context.Background(), args, &out, &errors, nil, loader)
			if code != 2 || out.Len() != 0 || !strings.Contains(errors.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
			}
			if strings.Contains(errors.String(), "configuration:") {
				t.Fatalf("syntax error loaded config: %q", errors.String())
			}
		})
	}
}

func TestCobraAggregateExplicitEmptyFromRejectsAsOperationError(t *testing.T) {
	var out, errors bytes.Buffer
	code := Run(context.Background(), []string{"apply", "--from="}, &out, &errors, nil, &fakeConfigLoader{})
	if code != 1 || out.Len() != 0 || !strings.Contains(errors.String(), "regular file path") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
	}
}
