package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type exampleItem struct {
	Name string `json:"name"`
}

type exampleList struct {
	TypeMeta
	Items []exampleItem `json:"items"`
}

func TestTypedListEnvelopeAndEmptyItems(t *testing.T) {
	value := exampleList{TypeMeta: NewTypeMeta("ExampleList"), Items: []exampleItem{}}
	var _ Document = value

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"apiVersion":"aginctus.dev/v1alpha1","kind":"ExampleList","items":[]}`
	if string(encoded) != want {
		t.Fatalf("got %s, want %s", encoded, want)
	}
	if meta := value.TypeMetadata(); meta.Kind != "ExampleList" || meta.APIVersion != APIVersionV1Alpha1 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestTypedErrorsAreInspectableAndSerializable(t *testing.T) {
	cause := NewError(CodeNotFound, "workload not found")
	var typed *Error
	if !errors.As(fmt.Errorf("read workload: %w", cause), &typed) || typed.Code != CodeNotFound {
		t.Fatalf("typed error not inspectable: %+v", typed)
	}
	if got := cause.Error(); got != "workload not found" {
		t.Fatalf("unexpected error text: %s", got)
	}
	data, err := json.Marshal(cause)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"apiVersion":"aginctus.dev/v1alpha1"`, `"kind":"Error"`, `"code":"not_found"`} {
		if !strings.Contains(string(data), part) {
			t.Fatalf("missing %s in %s", part, data)
		}
	}
	var absent *Error
	if absent.Error() != "<nil>" {
		t.Fatalf("unexpected nil error text: %s", absent.Error())
	}
}
