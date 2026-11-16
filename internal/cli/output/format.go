// Package output contains shared CLI format selection and machine encoding.
// It never selects an environment, invokes domain operations, or determines
// authorization. Human table and detail views belong to command renderers.
package output

import (
	"fmt"
	"io"

	"encoding/json"

	"github.com/blevinn/aginctus/pkg/api"
	"sigs.k8s.io/yaml"
)

// Format is a requested CLI presentation style. Table, wide and name are
// human/script projections with command-specific implementations.
type Format string

const (
	Table Format = "table"
	Wide  Format = "wide"
	JSON  Format = "json"
	YAML  Format = "yaml"
	Name  Format = "name"
)

// ParseFormat recognizes the MVP output vocabulary. An omitted flag uses
// the concise human view; individual commands decide if a format applies.
func ParseFormat(raw string) (Format, error) {
	switch Format(raw) {
	case "":
		return Table, nil
	case Table, Wide, JSON, YAML, Name:
		return Format(raw), nil
	default:
		return "", fmt.Errorf("unsupported output format %q (valid: table, wide, json, yaml, name)", raw)
	}
}

// WriteStructured emits one complete result document as JSON or YAML.
// Encoding completes before writing, so serializer errors never emit partial
// output. Documents are prevalidated public projections, not raw domain
// structs; this function is not a redaction or authorization boundary.
func WriteStructured(w io.Writer, format Format, document api.Document) error {
	if format != JSON && format != YAML {
		return fmt.Errorf("output format %q is not a structured format", format)
	}
	if w == nil || document == nil {
		return fmt.Errorf("structured output requires a writer and document")
	}
	meta := document.TypeMetadata()
	if meta.APIVersion == "" || meta.Kind == "" {
		return fmt.Errorf("structured output requires apiVersion and kind")
	}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode structured output: %w", err)
	}
	if format == YAML {
		encoded, err = yaml.JSONToYAML(encoded)
		if err != nil {
			return fmt.Errorf("encode YAML output: %w", err)
		}
	}
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		encoded = append(encoded, '\n')
	}
	if _, err := w.Write(encoded); err != nil {
		return fmt.Errorf("write structured output: %w", err)
	}
	return nil
}
