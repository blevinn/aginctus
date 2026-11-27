package environment

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProtectedGatewayGrantStrictSelection(t *testing.T) {
	g := ProtectedGatewayGrant{APIVersion: "aginctus.dev/protected-gateway-grant/v1alpha1",
		ServerID: "sha256:"+strings.Repeat("a", 64), Project: "workloads",
		EnvironmentID: "11111111-1111-4111-8111-111111111111", RevisionID: strings.Repeat("b", 64), ProtectedProject: "gateway-private"}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := parseProtectedGatewayGrant(raw); err != nil || got != g {
		t.Fatal("valid grant rejected")
	}
	for _, input := range []string{
		strings.Replace(string(raw), "gateway-private", "workloads", 1),
		strings.Replace(string(raw), "gateway-private", "default", 1),
		strings.TrimSuffix(string(raw), "}") + `,"protectedProject":"other"}`,
		strings.TrimSuffix(string(raw), "}") + `,"credentialPath":"/etc/shadow"}`,
	} {
		if _, err := parseProtectedGatewayGrant([]byte(input)); err == nil {
			t.Fatal("invalid protected grant accepted")
		}
	}
}
