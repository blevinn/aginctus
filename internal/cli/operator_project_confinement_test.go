package cli

import (
	"context"
	"strings"
	"testing"
)

func TestInspectProjectConfinementGetOnlyAndFailClosed(t *testing.T) {
	path := "query --raw --request GET /1.0/projects/sandbox"
	for _, tc := range []struct {
		name string
		data []byte
		want projectConfinementEvidence
	}{
		{"present", operatorAPIResponse(`{"name":"sandbox","config":{"restricted":"true","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}`), projectConfinementNetwork},
		{"denied", []byte(`{"type":"error","error_code":403,"error":"private-token"}`), projectConfinementUnknown},
		{"malformed", []byte("private-token"), projectConfinementUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubOperatorIncus(t, func(args ...string) ([]byte, error) {
				if strings.Join(args, " ") != path {
					t.Fatalf("unexpected call: %#v", args)
				}
				return tc.data, nil
			})
			if got := inspectProjectConfinement(context.Background(), "sandbox"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
