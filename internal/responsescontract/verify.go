// Package responsescontract validates an observed Responses SSE transcript
// without storing or echoing any of its potentially sensitive event payloads.
package responsescontract

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	maxStreamBytes = 8 << 20
	maxLineBytes   = 1 << 20
)

type Summary struct {
	Events        int
	FunctionCalls int
}

type eventPayload struct {
	Type     string `json:"type"`
	Response struct {
		Status string `json:"status"`
	} `json:"response"`
	Item struct {
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
}

// Verify checks event framing, consistency, terminal completion and optionally
// a fully assembled function call. It does not make HTTP requests or establish
// provider provenance, authentication, model entitlement or tool execution.
// Failure messages never include event bodies, upstream errors or prompts.
func Verify(input io.Reader, requireFunctionCall bool) (Summary, error) {
	var summary Summary
	scanner := bufio.NewScanner(io.LimitReader(input, maxStreamBytes+1))
	scanner.Buffer(make([]byte, 4096), maxLineBytes)

	eventName := ""
	var data strings.Builder
	hasData, completed, done := false, false, false
	dispatch := func() error {
		if !hasData {
			eventName = ""
			data.Reset()
			return nil
		}
		body := data.String()
		eventNameLocal := eventName
		eventName = ""
		data.Reset()
		hasData = false
		if body == "[DONE]" {
			if !completed || done || eventNameLocal != "" {
				return fmt.Errorf("unexpected Responses stream end marker")
			}
			done = true
			return nil
		}
		if completed || done {
			return fmt.Errorf("Responses events followed terminal completion")
		}
		var payload eventPayload
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			return fmt.Errorf("malformed Responses event JSON")
		}
		if !strings.HasPrefix(payload.Type, "response.") {
			return fmt.Errorf("missing Responses event type")
		}
		if eventNameLocal != "" && eventNameLocal != payload.Type {
			return fmt.Errorf("Responses SSE event name and payload type differ")
		}
		summary.Events++
		switch payload.Type {
		case "response.completed":
			if payload.Response.Status != "completed" {
				return fmt.Errorf("Responses completion has non-completed status")
			}
			completed = true
		case "response.failed", "response.incomplete":
			return fmt.Errorf("Responses stream failed or was incomplete")
		case "response.output_item.done":
			if payload.Item.Type == "function_call" {
				if payload.Item.CallID == "" || payload.Item.Name == "" ||
					!json.Valid([]byte(payload.Item.Arguments)) {
					return fmt.Errorf("Responses function-call item is incomplete")
				}
				summary.FunctionCalls++
			}
		}
		return nil
	}

	bytesRead := 0
	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += len(line) + 1
		if bytesRead > maxStreamBytes {
			return Summary{}, fmt.Errorf("Responses stream exceeds allowed size")
		}
		if line == "" {
			if err := dispatch(); err != nil {
				return Summary{}, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // SSE keep-alive comment.
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			field = line
			value = ""
		} else if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		switch field {
		case "event":
			if eventName != "" {
				return Summary{}, fmt.Errorf("duplicate SSE event name")
			}
			eventName = value
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			hasData = true
		case "id", "retry":
			// Valid SSE metadata, not part of the Responses payload.
		default:
			return Summary{}, fmt.Errorf("unsupported SSE field")
		}
	}
	if err := scanner.Err(); err != nil {
		return Summary{}, fmt.Errorf("unable to read bounded Responses stream")
	}
	if err := dispatch(); err != nil {
		return Summary{}, err
	}
	if !completed {
		return Summary{}, fmt.Errorf("Responses stream has no terminal completion")
	}
	if requireFunctionCall && summary.FunctionCalls == 0 {
		return Summary{}, fmt.Errorf("Responses stream has no completed function call")
	}
	return summary, nil
}
