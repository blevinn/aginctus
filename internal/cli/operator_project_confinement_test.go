package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyProjectConfinementDoesNotAuthorizeFromPolicyFlags(t *testing.T) {
	confined := `{"name":"sandbox","config":{"restricted":"true","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"true"}}`
	for _, tc := range []struct {
		name string
		raw string
		want projectConfinementEvidence
	}{
		{"candidate not authorization", confined, projectConfinementCandidate},
		{"network inheritance", strings.Replace(confined, `"features.networks":"true"`, `"features.networks":"false"`, 1), projectConfinementNetwork},
		{"image inheritance", strings.Replace(confined, `"features.images":"true"`, `"features.images":"false"`, 1), projectConfinementShared},
		{"profile inheritance", strings.Replace(confined, `"features.profiles":"true"`, `"features.profiles":"false"`, 1), projectConfinementShared},
		{"volume inheritance", strings.Replace(confined, `"features.storage.volumes":"true"`, `"features.storage.volumes":"false"`, 1), projectConfinementShared},
		{"unrestricted project", strings.Replace(confined, `"restricted":"true"`, `"restricted":"false"`, 1), projectConfinementUnrestricted},
		{"missing restriction", strings.Replace(confined, `"restricted":"true",`, "", 1), projectConfinementUnknown},
		{"missing network flag", strings.Replace(confined, `,"features.networks":"true"`, "", 1), projectConfinementUnknown},
		{"project mismatch", strings.Replace(confined, `"name":"sandbox"`, `"name":"foreign"`, 1), projectConfinementUnknown},
		{"malformed", "not-json", projectConfinementUnknown},
		{"null", "null", projectConfinementUnknown},
		{"no config", `{"name":"sandbox"}`, projectConfinementUnknown},
		{"empty defaulted policy", `{"name":"sandbox","config":{}}`, projectConfinementUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyProjectConfinement(json.RawMessage(tc.raw), "sandbox"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

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
