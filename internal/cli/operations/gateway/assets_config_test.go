package gatewayops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAssetConfigInlineAndDefault(t *testing.T) {
	for _, tc := range []struct {
		name, inline, want string
	}{
		{"empty defaults to object", "", "{}"},
		{"inline JSON object", `{"transport":"streamable-http"}`, `{"transport":"streamable-http"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got json.RawMessage
			if err := ReadAssetConfig(tc.inline, "", &got); err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("decoded %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadAssetConfigRejectsUnsafeFileInputs(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.json")
	large := filepath.Join(dir, "large.json")
	symlink := filepath.Join(dir, "link.json")
	if err := os.WriteFile(valid, []byte(`{"transport":"streamable-http"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, []byte("{"+strings.Repeat("x", 1<<20)+"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(valid, symlink); err != nil {
		t.Fatal(err)
	}
	var good json.RawMessage
	if err := ReadAssetConfig("", valid, &good); err != nil || string(good) != `{"transport":"streamable-http"}` {
		t.Fatalf("regular JSON file: %q, %v", good, err)
	}
	for _, path := range []string{symlink, dir, large, filepath.Join(dir, "missing.json")} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var got json.RawMessage
			err := ReadAssetConfig("", path, &got)
			if err == nil || strings.Contains(err.Error(), path) {
				t.Fatalf("unsafe file %q accepted or revealed host path: %v", path, err)
			}
		})
	}
}

func TestReadAssetConfigRejectsMalformedOrExcessiveInlineData(t *testing.T) {
	for _, inline := range []string{`[]`, `{"a":1} {"b":2}`, "{", strings.Repeat("x", (1<<20)+1)} {
		var got json.RawMessage
		if err := ReadAssetConfig(inline, "", &got); err == nil {
			t.Fatalf("accepted invalid inline payload of %d bytes", len(inline))
		}
	}
}
