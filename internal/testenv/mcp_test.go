package testenv

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const mcpMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

func rpc(t *testing.T, method, otherParams string) string {
	t.Helper()
	params := "{" + mcpMeta
	if otherParams != "" {
		params += "," + otherParams
	}
	return `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + "}}"
}

func invokeMCP(t *testing.T, requests ...string) ([]map[string]any, error) {
	t.Helper()
	var out bytes.Buffer
	err := ServeMCP(context.Background(), strings.NewReader(strings.Join(requests, "\n")+"\n"), &out)
	var results []map[string]any
	for _, frame := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if frame == "" {
			continue
		}
		var obj map[string]any
		if parseErr := json.Unmarshal([]byte(frame), &obj); parseErr != nil {
			t.Fatalf("MCP stdout contains non-JSON: %q: %v", frame, parseErr)
		}
		results = append(results, obj)
	}
	return results, err
}

func TestMCPDiscoveryAndReadOnlyTools(t *testing.T) {
	results, err := invokeMCP(t,
		rpc(t, "server/discover", ""),
		rpc(t, "tools/list", ""),
		rpc(t, "tools/call", `"name":"vm_list_scenarios","arguments":{}`),
		rpc(t, "tools/call", `"name":"vm_plan","arguments":{"scenario":"smoke"}`),
	)
	if err != nil || len(results) != 4 {
		t.Fatalf("frames=%d err=%v", len(results), err)
	}
	discovery := results[0]["result"].(map[string]any)
	if discovery["resultType"] != "complete" || discovery["supportedVersions"].([]any)[0] != mcpVersion {
		t.Fatalf("unexpected discovery: %#v", discovery)
	}
	listed := results[1]["result"].(map[string]any)
	if listed["cacheScope"] != "private" || len(listed["tools"].([]any)) != 2 {
		t.Fatalf("unexpected tools: %#v", listed)
	}
	names := listed["tools"].([]any)
	if names[0].(map[string]any)["name"] != "vm_list_scenarios" || names[1].(map[string]any)["name"] != "vm_plan" {
		t.Fatalf("unexpected tools: %#v", names)
	}
	items := results[2]["result"].(map[string]any)
	if len(items["structuredContent"].(map[string]any)["scenarios"].([]any)) != 4 {
		t.Fatalf("unexpected scenarios: %#v", items)
	}
	plan := results[3]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if plan["target"] != "vm-harness-smoke" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestMCPRejectsHostExecutionAndUnrecognizedInputs(t *testing.T) {
	for _, input := range []string{
		rpc(t, "tools/call", `"name":"vm_test","arguments":{"scenario":"smoke"}`),
		rpc(t, "tools/call", `"name":"vm_exec","arguments":{"argv":["/bin/sh"]}`),
		rpc(t, "tools/call", `"name":"vm_plan","arguments":{"scenario":"../evil"}`),
		rpc(t, "tools/call", `"name":"vm_plan","arguments":{"scenario":"smoke","shell":"id"}`),
		rpc(t, "tools/call", `"name":"vm_list_scenarios","arguments":{"cmd":"id"}`),
		rpc(t, "tools/call", `"name":"vm_list_scenarios","arguments":null`),
		rpc(t, "tools/list", `"cursor":"next"`),
		rpc(t, "vm/destroy", `"runId":"other"`),
	} {
		results, err := invokeMCP(t, input)
		if err != nil || len(results) != 1 || results[0]["error"] == nil {
			t.Fatalf("expected one protocol error: %#v %v", results, err)
		}
	}
}

func TestMCPVersionAndMetadataFailClosed(t *testing.T) {
	input := rpc(t, "tools/list", "")
	for _, bad := range []string{
		strings.Replace(input, "2026-07-28", "2025-11-25", 1),
		strings.Replace(input, `"io.modelcontextprotocol/clientCapabilities":{}`, `"ignored":{}`, 1),
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`not-json`,
	} {
		results, err := invokeMCP(t, bad)
		if err != nil || len(results) != 1 || results[0]["error"] == nil {
			t.Fatalf("expected rejection of %q: %#v %v", bad, results, err)
		}
	}
}

func TestMCPNotificationsHaveNoOutput(t *testing.T) {
	request := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}`
	results, err := invokeMCP(t, request+"}")
	if err != nil || len(results) != 0 {
		t.Fatalf("notifications must have no response: %#v %v", results, err)
	}
}

func TestMCPOversizedFramesCannotRunAnything(t *testing.T) {
	_, err := invokeMCP(t, strings.Repeat("X", maxMCPMessage+64))
	if err == nil {
		t.Fatal("oversized frame was accepted")
	}
}
