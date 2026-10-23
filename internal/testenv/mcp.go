package testenv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const mcpVersion = "2026-07-28"
const maxMCPMessage = 65536

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

func mcpServerInfo() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/serverInfo": map[string]string{
			"name": "aginctus-testenv",
			"version": "0.1.0",
		},
	}
}

func mcpRequestError(id json.RawMessage, code int, message string, data any) mcpResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: code, Message: message, Data: data}}
}

func decodeStrict(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return errors.New("missing object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected JSON remainder")
	}
	return nil
}

func validateMCPMeta(params json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(params, &obj); err != nil || obj == nil {
		return errors.New("params must be an object")
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(obj["_meta"], &meta); err != nil || meta == nil {
		return errors.New("missing MCP request metadata")
	}
	var version string
	if err := json.Unmarshal(meta["io.modelcontextprotocol/protocolVersion"], &version); err != nil {
		return errors.New("missing MCP protocol version")
	}
	if version != mcpVersion {
		return fmt.Errorf("unsupported MCP protocol version")
	}
	var capabilities map[string]json.RawMessage
	if err := json.Unmarshal(meta["io.modelcontextprotocol/clientCapabilities"], &capabilities); err != nil || capabilities == nil {
		return errors.New("missing MCP client capabilities")
	}
	return nil
}

func mcpTools() []map[string]any {
	return []map[string]any{
		{
			"name": "vm_list_scenarios",
			"description": "List fixed disposable NixOS VM batch scenarios; offline and read-only.",
			"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
			"annotations": map[string]any{"readOnlyHint": true},
		},
		{
			"name": "vm_plan",
			"description": "Return the pinned Nix test derivation for one known VM scenario. Does not start a VM.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{"scenario": map[string]string{"type": "string"}},
				"required": []string{"scenario"},
				"additionalProperties": false,
			},
			"annotations": map[string]any{"readOnlyHint": true},
		},
	}
}

func mcpContent(data any) any {
	encoded, _ := json.Marshal(data) // values are internally constructed
	return map[string]any{
		"resultType": "complete",
		"content": []any{map[string]string{"type": "text", "text": string(encoded)}},
		"structuredContent": data,
		"_meta": mcpServerInfo(),
	}
}

func validMCPRequestID(raw json.RawMessage) bool {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	switch id := value.(type) {
	case string:
		return true
	case json.Number:
		// JSON-RPC IDs are strings or integral numbers, not booleans,
		// arrays, objects, null, or fractional floating-point numbers.
		var integer int64
		_, err := fmt.Sscan(string(id), &integer)
		return err == nil && fmt.Sprint(integer) == string(id)
	default:
		return false
	}
}

func mcpAnswer(raw []byte) *mcpResponse {
	var request mcpRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		answer := mcpRequestError(nil, -32700, "Parse error", nil)
		return &answer
	}
	// JSON-RPC notifications have no ID and must never produce replies.
	if len(request.ID) == 0 {
		return nil
	}
	if request.JSONRPC != "2.0" || request.Method == "" || !validMCPRequestID(request.ID) {
		answer := mcpRequestError(request.ID, -32600, "Invalid Request", nil)
		return &answer
	}
	if err := validateMCPMeta(request.Params); err != nil {
		answer := mcpRequestError(request.ID, -32602, err.Error(), nil)
		return &answer
	}
	response := mcpResponse{JSONRPC: "2.0", ID: request.ID}
	var params map[string]json.RawMessage
	_ = json.Unmarshal(request.Params, &params) // validated above
	switch request.Method {
	case "server/discover":
		if len(params) != 1 {
			answer := mcpRequestError(request.ID, -32602, "unexpected discovery arguments", nil)
			return &answer
		}
		response.Result = map[string]any{
			"resultType": "complete",
			"supportedVersions": []string{mcpVersion},
			"capabilities": map[string]any{"tools": map[string]any{}},
			"instructions": "Read-only VM discovery. No guest execution or host access available via MCP.",
			"_meta": mcpServerInfo(),
			"ttlMs": 60000,
			"cacheScope": "private",
		}
	case "tools/list":
		if len(params) != 1 {
			answer := mcpRequestError(request.ID, -32602, "unexpected list arguments", nil)
			return &answer
		}
		response.Result = map[string]any{
			"resultType": "complete", "tools": mcpTools(),
			"ttlMs": 60000, "cacheScope": "private", "_meta": mcpServerInfo(),
		}
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		// _meta is independently validated; it is not a source of authority.
		copyParams := make(map[string]json.RawMessage, len(params))
		for key, value := range params {
			if key != "_meta" {
				copyParams[key] = value
			}
		}
		serialized, _ := json.Marshal(copyParams)
		if err := decodeStrict(serialized, &call); err != nil {
			answer := mcpRequestError(request.ID, -32602, "invalid tool call parameters", nil)
			return &answer
		}
		switch call.Name {
		case "vm_list_scenarios":
			if len(call.Arguments) != 0 {
				var arguments map[string]json.RawMessage
				if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments == nil || len(arguments) != 0 {
					answer := mcpRequestError(request.ID, -32602, "vm_list_scenarios accepts no arguments", nil)
					return &answer
				}
			}
			response.Result = mcpContent(map[string]any{"scenarios": VMScenarios()})
		case "vm_plan":
			var args struct { Scenario string `json:"scenario"` }
			if err := decodeStrict(call.Arguments, &args); err != nil {
				answer := mcpRequestError(request.ID, -32602, "invalid vm_plan arguments", nil)
				return &answer
			}
			scenario, err := VMScenario(args.Scenario)
			if err != nil {
				answer := mcpRequestError(request.ID, -32602, "unknown VM scenario", nil)
				return &answer
			}
			response.Result = mcpContent(scenario)
		default:
			answer := mcpRequestError(request.ID, -32601, "Unknown or unavailable testenv tool", nil)
			return &answer
		}
	default:
		answer := mcpRequestError(request.ID, -32601, "Method not found", nil)
		return &answer
	}
	return &response
}

// ServeMCP is a read-only, stdio-only MCP 2026-07-28 implementation.
// It does not invoke the Python runner, spawn Nix, or contact host Incus.
func ServeMCP(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), maxMCPMessage)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		response := mcpAnswer(scanner.Bytes())
		if response == nil {
			continue
		}
		frame, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("serialize MCP response: %w", err)
		}
		if _, err := writer.Write(append(frame, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("MCP input closed due to invalid/oversized frame: %w", err)
	}
	return ctx.Err()
}
