package environment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restrictedOperatorConfigFixture() RestrictedOperatorReadConfig {
	return RestrictedOperatorReadConfig{
		APIVersion: RestrictedOperatorReadConfigVersion,
		Endpoint: "https://incus.test:8443", Project: "team",
		ServerCertificatePEM: "server-certificate",
		TrustedServerSHA256: strings.Repeat("a", 64),
		ClientCertificatePEM: "client-certificate",
		ClientPrivateKeyPEM: "client-private-key",
	}
}

func writeRestrictedConfig(t *testing.T, data []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "restricted.json")
	if err := os.WriteFile(path, data, mode); err != nil { t.Fatal(err) }
	return path
}

func TestRestrictedOperatorConfigRequiresPrivateExplicitRegularFile(t *testing.T) {
	encoded, err := json.Marshal(restrictedOperatorConfigFixture())
	if err != nil { t.Fatal(err) }
	path := writeRestrictedConfig(t, encoded, 0o600)
	got, err := ReadRestrictedOperatorReadConfig(path)
	if err != nil || got.Connection().Project != "team" ||
		got.Connection().ClientPrivateKeyPEM != "client-private-key" {
		t.Fatalf("private read=%+v err=%v", got, err)
	}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604} {
		if err := os.Chmod(path, mode); err != nil { t.Fatal(err) }
		if got, err := ReadRestrictedOperatorReadConfig(path); err == nil ||
			got.ClientPrivateKeyPEM != "" {
			t.Fatalf("nonprivate %v: %+v err=%v", mode, got, err)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil { t.Fatal(err) }
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, link); err != nil { t.Fatal(err) }
	if _, err := ReadRestrictedOperatorReadConfig(link); err == nil {
		t.Fatal("symlinked operator credentials accepted")
	}
	if _, err := ReadRestrictedOperatorReadConfig("relative.json"); err == nil {
		t.Fatal("relative operator config accepted")
	}
	if _, err := ReadRestrictedOperatorReadConfig(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing config accepted")
	}
}

func TestRestrictedOperatorConfigRejectsAmbiguousOrForeignData(t *testing.T) {
	encoded, err := json.Marshal(restrictedOperatorConfigFixture())
	if err != nil { t.Fatal(err) }
	for _, tc := range []struct{name string; data []byte}{
		{"unknown", []byte(strings.Replace(string(encoded), `"project":"team"`, `"project":"team","admin":true`, 1))},
		{"duplicate", []byte(strings.Replace(string(encoded), `"project":"team"`, `"project":"team","project":"other"`, 1))},
		{"null", []byte(strings.Replace(string(encoded), `"project":"team"`, `"project":null`, 1))},
		{"trailing", append(append([]byte{}, encoded...), []byte("{}")...)},
		{"oversized", []byte(strings.Repeat("a", maxRestrictedOperatorConfigBytes+1))},
		{"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeRestrictedConfig(t, tc.data, 0o600)
			value, err := ReadRestrictedOperatorReadConfig(path)
			if err == nil || value.ClientPrivateKeyPEM != "" {
				t.Fatalf("invalid config accepted: %+v err=%v", value, err)
			}
		})
	}
}
