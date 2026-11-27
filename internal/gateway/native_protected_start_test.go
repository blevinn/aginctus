package gateway

import (
	"strings"
	"testing"
)

func TestProtectedLauncherRejectsExtraOptions(t *testing.T) {
	pg := "ag-" + strings.Repeat("a", 32) + "-pg"
	want := gatewayLauncher(NativeGatewayInstanceSpec{Role: "litellm"}, pg)
	if !gatewayLauncherConfigSafe(want, want, true) {
		t.Fatal("fixed launcher rejected")
	}
	for _, key := range []string{"environment.LITELLM_MASTER_KEY", "raw.lxc", "security.privileged", "oci.extra"} {
		copy := gatewayLauncher(NativeGatewayInstanceSpec{Role: "litellm"}, pg)
		copy[key] = "unexpected"
		if gatewayLauncherConfigSafe(copy, want, true) {
			t.Fatal("unsafe option accepted")
		}
	}
}
