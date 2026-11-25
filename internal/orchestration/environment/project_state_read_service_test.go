package environment

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectReadServiceConfigFixture() ProjectStateReadServiceConfig {
	return ProjectStateReadServiceConfig{
		APIVersion: ProjectStateReadServiceConfigVersion,
		Project: "team", ListenAddress: "127.0.0.1:9443",
		MountPath: "/srv/aginctus-state",
		OperatorConfigFile: "/run/aginctus/private-operator.json",
		ServiceCertificatePEM: "server PEM",
		ServicePrivateKeyPEM: "server key",
		TrustedClientCertificatesPEM: "approved client PEM",
	}
}

func writeProjectServiceConfig(t *testing.T, data []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "service.json")
	if err := os.WriteFile(path, data, mode); err != nil { t.Fatal(err) }
	return path
}

func TestProjectStateServiceConfigIsPrivateStrictAndExplicit(t *testing.T) {
	value := projectReadServiceConfigFixture()
	raw, err := json.Marshal(value)
	if err != nil { t.Fatal(err) }
	path := writeProjectServiceConfig(t, raw, 0o600)
	got, err := ReadProjectStateReadServiceConfig(path)
	if err != nil || got != value {
		t.Fatalf("read service config=%+v err=%v", got, err)
	}
	if err := os.Chmod(path, 0o644); err != nil { t.Fatal(err) }
	if got, err := ReadProjectStateReadServiceConfig(path); err == nil || got.ServicePrivateKeyPEM != "" {
		t.Fatalf("world-readable service TLS key accepted: %+v %v", got, err)
	}
	if err := os.Chmod(path, 0o600); err != nil { t.Fatal(err) }
	link := filepath.Join(t.TempDir(), "service-link")
	if err := os.Symlink(path, link); err != nil { t.Fatal(err) }
	if _, err := ReadProjectStateReadServiceConfig(link); err == nil {
		t.Fatal("service config symlink accepted")
	}
	for _, broken := range []string{
		strings.Replace(string(raw), `"project":"team"`, `"project":"default"`, 1),
		strings.Replace(string(raw), `"project":"team"`, `"project":"team","project":"foreign"`, 1),
		strings.Replace(string(raw), `"project":"team"`, `"project":"team","rootAdmin":true`, 1),
		strings.Replace(string(raw), `"project":"team"`, `"project":null`, 1),
		string(raw) + "{}",
	} {
		if _, err := ReadProjectStateReadServiceConfig(writeProjectServiceConfig(t, []byte(broken), 0o600)); err == nil {
			t.Fatalf("malformed service config accepted")
		}
	}
}

func TestProjectStateServiceListenerRejectsPublicAndAmbientBindings(t *testing.T) {
	for _, addr := range []string{"", ":8443", "0.0.0.0:8443",
		"[::]:8443", "example.com:8443", "8.8.8.8:8443",
		"127.0.0.1:0", "127.0.0.1:99999", "unix:/tmp/state.sock"} {
		if err := validateProjectStateReadListener(addr); err == nil {
			t.Errorf("unsafe listener %q accepted", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8443", "10.100.20.3:8443", "[::1]:8443"} {
		if err := validateProjectStateReadListener(addr); err != nil {
			t.Errorf("private listener %q rejected: %v", addr, err)
		}
	}
}

func TestProjectStateServiceFailsBeforeOpeningUnauthenticatedListener(t *testing.T) {
	c := projectReadServiceConfigFixture()
	// No valid operator credential is available. A valid service config
	// must never bootstrap one or open an unauthenticated TCP listener.
	if err := ServeMountedProjectStateReads(context.Background(), c); err == nil {
		t.Fatal("service started without independent Incus credentials and mounted state")
	}
	var nilCtx context.Context
	if err := ServeMountedProjectStateReads(nilCtx, c); err == nil {
		t.Fatal("service started without context")
	}
}
