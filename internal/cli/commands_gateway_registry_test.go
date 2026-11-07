package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestGatewayRegistryLegacyMapping(t *testing.T) {
	tests := []struct{ source, want []string }{
		{[]string{"gateway", "model", "coding", "register", "--dry-run"}, []string{"gateway", "model", "register", "coding", "--dry-run"}},
		{[]string{"gateway", "model", "coding", "show"}, []string{"gateway", "model", "show", "coding"}},
		{[]string{"gateway", "model", "coding", "render"}, []string{"gateway", "model", "render", "coding"}},
		{[]string{"gateway", "provider", "chatgpt", "register"}, []string{"gateway", "provider", "register", "chatgpt"}},
		{[]string{"gateway", "provider", "alias", "show"}, []string{"gateway", "provider", "show", "alias"}},
		{[]string{"gateway", "model", "register", "coding"}, []string{"gateway", "model", "register", "coding"}},
	}
	for _, tc := range tests {
		if got := normalizeGatewayRegistryLegacyArgs(tc.source); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("got %q; want %q", got, tc.want)
		}
	}
}

func TestGatewayRegistryCobraSyntaxBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"gateway", "model", "show"},
		{"gateway", "model", "render", "a", "b"},
		{"gateway", "provider", "list", "--provider=x"},
		{"gateway", "provider", "register"},
		{"gateway", "model", "register", "a", "b"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := Run(context.Background(), args, &out, &stderr, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "configuration:") {
				t.Fatalf("configuration accessed: %q", stderr.String())
			}
		})
	}
}
