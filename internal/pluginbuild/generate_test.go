package pluginbuild

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateDeterministicComposition(t *testing.T) {
	manifest := Manifest{APIVersion: APIVersion, Package: "appplugins", Plugins: []Selection{
		{ImportPath: "example.org/z", Constructor: "New"},
		{ImportPath: "example.org/a", Constructor: "Create"},
	}}
	generate := func() []byte {
		t.Helper()
		raw, _ := json.Marshal(manifest)
		out, err := Generate(raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := generate()
	manifest.Plugins[0], manifest.Plugins[1] = manifest.Plugins[1], manifest.Plugins[0]
	if !bytes.Equal(first, generate()) {
		t.Fatal("generation depends on enumeration order")
	}
	source := string(first)
	if !strings.Contains(source, `selected0 "example.org/a"`) || !strings.Contains(source, "selected0.Create(),") || !strings.Contains(source, "return plugins.New(ctx,") {
		t.Fatalf("missing explicit composition:\n%s", source)
	}
	if strings.Contains(source, "func init(") {
		t.Fatal("implicit registration")
	}
}

func TestGenerateRejectsInvalidManifest(t *testing.T) {
	valid := `{"api_version":"` + APIVersion + `","package":"appplugins","plugins":[]}`
	for _, raw := range []string{
		`null`, `{}`, valid + `{}`, strings.Replace(valid, `"plugins":[]`, `"unknown":true`, 1),
		strings.Replace(valid, APIVersion, "future", 1),
		strings.Replace(valid, "appplugins", "for", 1),
		strings.Replace(valid, "appplugins", "_", 1),
	} {
		if _, err := Generate([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, selection := range []Selection{
		{"bad\";panic()", "New"}, {"../escape", "New"}, {"x//y", "New"}, {"x/", "New"},
		{"example.org/x", "new"}, {"example.org/x", "New()"}, {"example.org/x", "New\n"},
	} {
		raw, _ := json.Marshal(Manifest{APIVersion: APIVersion, Package: "appplugins", Plugins: []Selection{selection}})
		if _, err := Generate(raw); err == nil {
			t.Errorf("accepted %+v", selection)
		}
	}
	raw, _ := json.Marshal(Manifest{APIVersion: APIVersion, Package: "appplugins", Plugins: []Selection{{"example.org/x", "New"}, {"example.org/x", "New"}}})
	if _, err := Generate(raw); err == nil {
		t.Fatal("accepted duplicate factory")
	}
}
