package responsescontract

import (
	"strings"
	"testing"
)

const (
	created  = "event: response.created\ndata: {\"type\":\"response.created\"}\n\n"
	finished = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	callDone = "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"edit_file\",\"arguments\":\"{}\"}}\n\n"
)

func TestVerifyResponsesTerminalCompletionAndFunctionCall(t *testing.T) {
	stream := ": keepalive\r\n\r\n" +
		strings.ReplaceAll(created+callDone+finished+"data: [DONE]\n\n", "\n", "\r\n")
	summary, err := Verify(strings.NewReader(stream), true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Events != 3 || summary.FunctionCalls != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	summary, err = Verify(strings.NewReader(created+finished), false)
	if err != nil || summary.Events != 2 || summary.FunctionCalls != 0 {
		t.Fatalf("terminal response without tools: %+v %v", summary, err)
	}
	// SSE event: headers are optional when the JSON event type is present.
	if _, err := Verify(strings.NewReader("data: {\"type\":\"response.created\"}\n\n"+finished), false); err != nil {
		t.Fatalf("valid data-only event rejected: %v", err)
	}
}

func TestVerifyResponsesRejectsFailuresWithoutLeakingPayload(t *testing.T) {
	failed := "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n"
	incomplete := "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n"
	invalidCall := strings.Replace(callDone, "call-1", "", 1)
	leak := "SECRET_DO_NOT_PRINT"
	cases := []struct {
		name   string
		stream string
		tools  bool
	}{
		{"empty stream", "", false},
		{"missing terminal", created, false},
		{"done without terminal", created + "data: [DONE]\n\n", false},
		{"failed event", created + failed, false},
		{"incomplete event", created + incomplete, false},
		{"wrong completion status", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"in_progress\"}}\n\n", false},
		{"truncated JSON", "event: response.completed\ndata: {\"type\":\n\n", false},
		{"event payload mismatch", "event: response.completed\ndata: {\"type\":\"response.created\"}\n\n" + finished, false},
		{"duplicate completion", finished + finished, false},
		{"event after completion", finished + created, false},
		{"function item incomplete", invalidCall + finished, true},
		{"no tool event", created + finished, true},
		{"duplicate completed function call", callDone + callDone + finished, true},
		{"nonobject function arguments", strings.Replace(callDone, "{}", "null", 1) + finished, true},
		{"unknown SSE field", "authorization: Bearer " + leak + "\n\n" + finished, false},
		{"unexpected data marker", "data: [DONE]\n\n" + finished, false},
		{"unexpected event type", "data: {\"type\":\"error\",\"detail\":\"" + leak + "\"}\n\n" + finished, false},
		{"malformed credentials", "data: " + leak + "\n\n", false},
		{"unbounded line", "data: " + strings.Repeat("x", (1<<20)+10) + "\n\n", false},
		{"unbounded stream", strings.Repeat(": "+strings.Repeat("x", 8192)+"\n", 1100), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary, err := Verify(strings.NewReader(tc.stream), tc.tools)
			if err == nil {
				t.Fatalf("accepted invalid stream: %+v", summary)
			}
			if strings.Contains(err.Error(), leak) || strings.Contains(err.Error(), "Bearer") {
				t.Fatal("untrusted payload or credentials appeared in error")
			}
		})
	}
}
