package operatorops

import (
	"encoding/json"
	"strings"
	"testing"
)

// Pure project confinement classification stays with the fail-closed policy.
func TestClassifyProjectConfinementDoesNotAuthorizeFromPolicyFlags(t *testing.T) {
	confined := `{"name":"sandbox","config":{"restricted":"true","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"true"}}`
	for _, tc := range []struct {
		name string
		raw string
		want ProjectConfinementEvidence
	}{
		{"candidate not authorization", confined, ProjectConfinementCandidate},
		{"network inheritance", strings.Replace(confined, `"features.networks":"true"`, `"features.networks":"false"`, 1), ProjectConfinementNetwork},
		{"image inheritance", strings.Replace(confined, `"features.images":"true"`, `"features.images":"false"`, 1), ProjectConfinementShared},
		{"profile inheritance", strings.Replace(confined, `"features.profiles":"true"`, `"features.profiles":"false"`, 1), ProjectConfinementShared},
		{"volume inheritance", strings.Replace(confined, `"features.storage.volumes":"true"`, `"features.storage.volumes":"false"`, 1), ProjectConfinementShared},
		{"unrestricted project", strings.Replace(confined, `"restricted":"true"`, `"restricted":"false"`, 1), ProjectConfinementUnrestricted},
		{"missing restriction", strings.Replace(confined, `"restricted":"true",`, "", 1), ProjectConfinementUnknown},
		{"missing network flag", strings.Replace(confined, `,"features.networks":"true"`, "", 1), ProjectConfinementUnknown},
		{"project mismatch", strings.Replace(confined, `"name":"sandbox"`, `"name":"foreign"`, 1), ProjectConfinementUnknown},
		{"malformed", "not-json", ProjectConfinementUnknown},
		{"null", "null", ProjectConfinementUnknown},
		{"no config", `{"name":"sandbox"}`, ProjectConfinementUnknown},
		{"empty defaulted policy", `{"name":"sandbox","config":{}}`, ProjectConfinementUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyProjectConfinement(json.RawMessage(tc.raw), "sandbox"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
