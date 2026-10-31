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
	if len(items["structuredContent"].(map[string]any)["scenarios"].([]any)) != 9 {
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
		strings.Replace(input, `"io.modelcontextprotocol/clientCapabilities":{}`, `"ignored":{}`, 1),
		strings.Replace(input, `"io.modelcontextprotocol/clientCapabilities":{}`, `"io.modelcontextprotocol/clientCapabilities":null`, 1),
		strings.Replace(input, `"io.modelcontextprotocol/clientCapabilities":{}`, `"io.modelcontextprotocol/clientCapabilities":[]`, 1),
		strings.Replace(input, `"2026-07-28"`, `null`, 1),
		strings.Replace(input, `"2026-07-28"`, `42`, 1),
		strings.Replace(input, `"io.modelcontextprotocol/protocolVersion":"2026-07-28",`, ``, 1),
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	} {
		results, err := invokeMCP(t, bad)
		if err != nil || len(results) != 1 || results[0]["error"].(map[string]any)["code"] != float64(-32602) {
			t.Fatalf("expected rejection of %q: %#v %v", bad, results, err)
		}
	}
}

func TestMCPLegacyInitializeExplainsSupportedVersion(t *testing.T) {
	results, err := invokeMCP(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`)
	if err != nil || len(results) != 1 {
		t.Fatalf("frames=%#v err=%v", results, err)
	}
	rpcError := results[0]["error"].(map[string]any)
	if rpcError["code"] != float64(-32602) || !strings.Contains(rpcError["message"].(string), mcpVersion) {
		t.Fatalf("legacy initialization must explain the supported version: %#v", results[0])
	}
}

func TestMCPUnsupportedVersionAndRetry(t *testing.T) {
	for _, method := range []string{"server/discover", "tools/list", "tools/call"} {
		for _, version := range []string{"2025-11-25", "1900-01-01"} {
			t.Run(method+"/"+version, func(t *testing.T) {
				params := ""
				if method == "tools/call" {
					params = `"name":"vm_list_scenarios","arguments":{}`
				}
				request := rpc(t, method, params)
				results, err := invokeMCP(t, strings.Replace(request, mcpVersion, version, 1), request)
				if err != nil || len(results) != 2 {
					t.Fatalf("frames=%#v err=%v", results, err)
				}
				rpcError := results[0]["error"].(map[string]any)
				data := rpcError["data"].(map[string]any)
				supported := data["supported"].([]any)
				if rpcError["code"] != float64(-32022) || data["requested"] != version || len(supported) != 1 || supported[0] != mcpVersion || results[0]["id"] != float64(1) {
					t.Fatalf("unexpected version error: %#v", results[0])
				}
				if results[1]["error"] != nil || results[1]["result"] == nil {
					t.Fatalf("retry failed: %#v", results[1])
				}
			})
		}
	}
}

func TestMCPEnvelopeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int
		id          any
	}{
		{"syntax", `{"jsonrpc":`, -32700, nil},
		{"trailing JSON", `{} {}`, -32700, nil},
		{"array", `[]`, -32600, nil},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, -32600, nil},
		{"null", `null`, -32600, nil},
		{"scalar", `42`, -32600, nil},
		{"empty object", `{}`, -32600, nil},
		{"missing method", `{"jsonrpc":"2.0","id":7}`, -32600, float64(7)},
		{"bad version", `{"jsonrpc":2,"id":"keep","method":"tools/list"}`, -32600, "keep"},
		{"bad method", `{"jsonrpc":"2.0","id":7,"method":[]}`, -32600, float64(7)},
		{"null method", `{"jsonrpc":"2.0","id":7,"method":null}`, -32600, float64(7)},
		{"wrong case", `{"JSONRPC":"2.0","id":7,"method":"tools/list"}`, -32600, float64(7)},
		{"invalid notification version", `{"jsonrpc":"1.0","method":"tools/list"}`, -32600, nil},
		{"invalid notification method", `{"jsonrpc":"2.0","method":42}`, -32600, nil},
		{"invalid notification params", `{"jsonrpc":"2.0","method":"tools/list","params":true}`, -32600, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, err := invokeMCP(t, tc.input)
			if err != nil || len(results) != 1 {
				t.Fatalf("frames=%#v err=%v", results, err)
			}
			if results[0]["jsonrpc"] != "2.0" || results[0]["id"] != tc.id || results[0]["error"].(map[string]any)["code"] != float64(tc.code) {
				t.Fatalf("unexpected error: %#v", results[0])
			}
		})
	}
}

func TestMCPInvalidIDsAndParams(t *testing.T) {
	for _, value := range []string{`[]`, `{}`, `null`, `true`, `false`, `1.5`} {
		request := strings.Replace(rpc(t, "tools/list", ""), `"id":1`, `"id":`+value, 1)
		results, err := invokeMCP(t, request)
		if err != nil || len(results) != 1 || results[0]["id"] != nil || results[0]["error"].(map[string]any)["code"] != float64(-32600) {
			t.Fatalf("invalid ID %s: %#v %v", value, results, err)
		}
	}
	for _, value := range []string{`[]`, `null`, `true`, `42`, `"wrong"`} {
		request := `{"jsonrpc":"2.0","id":"keep","method":"tools/list","params":` + value + `}`
		results, err := invokeMCP(t, request)
		if err != nil || len(results) != 1 || results[0]["id"] != "keep" || results[0]["error"].(map[string]any)["code"] != float64(-32600) {
			t.Fatalf("invalid params %s: %#v %v", value, results, err)
		}
	}
}

func TestMCPNotificationsHaveNoOutput(t *testing.T) {
	results, err := invokeMCP(t,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"unknown"}`,
		strings.Replace(rpc(t, "tools/call", `"name":"vm_exec","arguments":{}`), `"id":1,`, ``, 1),
	)
	if err != nil || len(results) != 0 {
		t.Fatalf("notifications must have no response: %#v %v", results, err)
	}
}

func TestMCPUnknownToolIsInvalidParams(t *testing.T) {
	results, err := invokeMCP(t, rpc(t, "tools/call", `"name":"vm_exec","arguments":{"argv":["/bin/sh"]}`))
	if err != nil || len(results) != 1 || results[0]["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatalf("unknown tool must be Invalid params: %#v %v", results, err)
	}
}

func TestMCPOversizedFramesCannotRunAnything(t *testing.T) {
	_, err := invokeMCP(t, strings.Repeat("X", maxMCPMessage+64))
	if err == nil {
		t.Fatal("oversized frame was accepted")
	}
}
