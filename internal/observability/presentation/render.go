package presentation

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

type renderer struct {
	writer io.Writer
	json   bool
	runID  string
}

// Text and JSONLines adapt the same vocabulary. They add no arbitrary messages,
// raw errors, guest output or configuration. Shape validation is not redaction;
// producers still own nonsecret metadata. Renderers are single-consumer sinks.
func Text(w io.Writer) Sink      { return &renderer{writer: w} }
func JSONLines(w io.Writer) Sink { return &renderer{writer: w, json: true} }

type record struct {
	SchemaVersion string             `json:"schema_version"`
	Type          string             `json:"type"`
	RunID         string             `json:"run_id"`
	Snapshot      *contract.Snapshot `json:"snapshot,omitempty"`
	Update        *contract.Update   `json:"update,omitempty"`
}

func (r *renderer) WriteSnapshot(s contract.Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if r.runID != "" && r.runID != s.RunID {
		return fmt.Errorf("renderer cannot mix runs")
	}
	r.runID = s.RunID
	if r.json {
		return r.writeJSON(record{SchemaVersion: contract.SchemaVersion, Type: "snapshot", RunID: r.runID, Snapshot: &s})
	}
	var text strings.Builder
	fmt.Fprintf(&text, "snapshot run=%q through=%d finished=%t history_dropped=%d\n", s.RunID, s.Watermark, s.Finished, s.Dropped)
	if s.Run != (contract.Event{}) {
		eventText(&text, "status", s.Run)
	}
	for _, e := range s.Entries {
		eventText(&text, "status", e)
	}
	return r.write([]byte(text.String()))
}

func (r *renderer) WriteUpdate(u contract.Update) error {
	if u.Gap {
		if u.Event != (contract.Event{}) || u.Watermark == 0 || r.runID == "" {
			return fmt.Errorf("invalid reporting gap")
		}
	} else if u.Event.Validate() != nil || u.Watermark != u.Event.Sequence {
		return fmt.Errorf("invalid reporting update")
	} else {
		if r.runID != "" && r.runID != u.Event.RunID {
			return fmt.Errorf("renderer cannot mix runs")
		}
		r.runID = u.Event.RunID
	}
	if r.json {
		return r.writeJSON(record{SchemaVersion: contract.SchemaVersion, Type: "update", RunID: r.runID, Update: &u})
	}
	var text strings.Builder
	if u.Gap {
		fmt.Fprintf(&text, "gap run=%q through=%d action=resnapshot\n", r.runID, u.Watermark)
	} else {
		eventText(&text, "event", u.Event)
	}
	return r.write([]byte(text.String()))
}

func eventText(w io.Writer, prefix string, e contract.Event) {
	// All untrusted strings are quoted; UTC formatting avoids custom zone names.
	fmt.Fprintf(w, "%s run=%q seq=%d time=%q kind=%q execution_state=%q reason=%q step=%q operation=%q scope=%q/%q/%q phase=%q check=%q attempt=%d required=%t revision=%q observed_at=%q activity=%q current=%d total=%d\n",
		prefix, e.RunID, e.Sequence, e.Time.UTC().Format(time.RFC3339Nano), e.Kind, e.State, e.Reason,
		e.StepID, e.OperationID, e.Scope.Deployment, e.Scope.Instance, e.Scope.Target, e.Phase, e.CheckID,
		e.Attempt, e.Required, e.AcceptedRevision, e.ObservedAt.UTC().Format(time.RFC3339Nano), e.Progress.Code, e.Progress.Current, e.Progress.Total)
}

func (r renderer) writeJSON(value record) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.write(append(data, '\n'))
}

func (r renderer) write(data []byte) error {
	n, err := r.writer.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
