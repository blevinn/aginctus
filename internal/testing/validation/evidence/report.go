package evidence

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON emits one schema-v1 report object followed by a newline.
func WriteJSON(output io.Writer, report Report) error {
	return json.NewEncoder(output).Encode(report)
}

// WriteVMText emits only normalized status and fixed rerun metadata.
func WriteVMText(output io.Writer, report Report) error {
	status, _ := report["status"].(string)
	results, _ := report["results"].([]any)
	for _, result := range results {
		fields, _ := result.(map[string]any)
		id, _ := fields["id"].(string)
		itemStatus, _ := fields["status"].(string)
		if _, err := fmt.Fprintf(output, "%s  %s\n", itemStatus, id); err != nil {
			return err
		}
		if itemStatus != "pass" {
			if rerun, ok := fields["rerun"].(string); ok {
				if _, err := fmt.Fprintf(output, "  To diagnose %s: %s\n", id, rerun); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintf(output, "VM evidence: %s\n", status)
	return err
}
