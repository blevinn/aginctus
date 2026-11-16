package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
	"sigs.k8s.io/yaml"
)

type fixture struct {
	api.TypeMeta
	Items []struct {
		Name string `json:"name"`
	} `json:"items"`
	Internal string `json:"-"`
}

func testDocument() fixture {
	return fixture{
		TypeMeta: api.NewTypeMeta("FixtureList"),
		Items:    []struct{ Name string `json:"name"` }{},
		Internal: "never-serialize",
	}
}

func TestParseFormat(t *testing.T) {
	cases := map[string]Format{
		"": Table, "table": Table, "wide": Wide,
		"json": JSON, "yaml": YAML, "name": Name,
	}
	for raw, want := range cases {
		got, err := ParseFormat(raw)
		if err != nil || got != want {
			t.Fatalf("ParseFormat(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"JSON", "csv", "jsonpath", "table,wide"} {
		if _, err := ParseFormat(raw); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestStructuredEncodingsPreserveJSONContract(t *testing.T) {
	document := testDocument()
	var jsonOut, yamlOut bytes.Buffer
	if err := WriteStructured(&jsonOut, JSON, document); err != nil {
		t.Fatal(err)
	}
	if err := WriteStructured(&yamlOut, YAML, document); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(jsonOut.Bytes(), []byte("\n")) || !bytes.HasSuffix(yamlOut.Bytes(), []byte("\n")) {
		t.Fatal("outputs must end in newline")
	}
	if bytes.Contains(jsonOut.Bytes(), []byte(document.Internal)) || bytes.Contains(yamlOut.Bytes(), []byte(document.Internal)) {
		t.Fatal("private field leaked")
	}
	fromYAML, err := yaml.YAMLToJSON(yamlOut.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var fromJSONValue, fromYAMLValue any
	if err := json.Unmarshal(jsonOut.Bytes(), &fromJSONValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fromYAML, &fromYAMLValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromJSONValue, fromYAMLValue) {
		t.Fatalf("JSON/YAML differ:\n%s\n%s", jsonOut.String(), yamlOut.String())
	}
	if !strings.Contains(jsonOut.String(), `"items": []`) {
		t.Fatalf("empty items must be an array: %s", jsonOut.String())
	}
}

func TestWriteStructuredRejectsInvalidInputsWithoutWriting(t *testing.T) {
	for _, format := range []Format{Table, Wide, Name, "csv"} {
		var out bytes.Buffer
		if err := WriteStructured(&out, format, testDocument()); err == nil || out.Len() != 0 {
			t.Errorf("expected error and no output for %s", format)
		}
	}
	var typedNil *fixture
	for _, document := range []api.Document{
		nil,
		typedNil,
		fixture{TypeMeta: api.TypeMeta{Kind: "FixtureList"}},
		fixture{TypeMeta: api.TypeMeta{APIVersion: api.APIVersionV1Alpha1}},
	} {
		var out bytes.Buffer
		if err := WriteStructured(&out, JSON, document); err == nil || out.Len() != 0 {
			t.Errorf("expected error and no output for %+v", document)
		}
	}
}

type rejectWriter struct{ err error }

func (w rejectWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteStructuredPreservesWriterError(t *testing.T) {
	expected := fmt.Errorf("output closed")
	err := WriteStructured(rejectWriter{err: expected}, JSON, testDocument())
	if !errors.Is(err, expected) {
		t.Fatalf("writer error lost: %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestWriteStructuredRejectsShortWrite(t *testing.T) {
	err := WriteStructured(shortWriter{}, JSON, testDocument())
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected short write, got %v", err)
	}
}
