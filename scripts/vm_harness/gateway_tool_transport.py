"""Validate native Responses function-call transport through the REAL LiteLLM.

This is a *transport* probe: function_call_output bodies are deliberately
fake. It never executes a tool or claims Herdr/OpenCode acceptance.
"""
from __future__ import annotations

import json
import re

NONCE = re.compile(r"[a-zA-Z0-9_-]{8,64}\Z")
STEP_TOOLS = ("read", "edit", "bash")
TOOL_DEFINITIONS = [
    {"type": "function", "name": "read",
     "parameters": {"type": "object", "properties": {
         "filePath": {"type": "string"}}}},
    {"type": "function", "name": "edit",
     "parameters": {"type": "object", "properties": {
         "filePath": {"type": "string"}, "oldString": {"type": "string"},
         "newString": {"type": "string"}}}},
    {"type": "function", "name": "bash",
     "parameters": {"type": "object", "properties": {
         "command": {"type": "string"}}}},
]


class TransportFailure(ValueError):
    """Safe failure label: never includes provider frames or guest data."""


def completed_item(raw: bytes, nonce: str, step: int):
    """Parse a bounded SSE stream and verify one terminal response item.

    Supports both event-prefixed and data-only SSE; the latter is emitted
    by some pinned LiteLLM paths. [DONE] is optional but if present must
    follow response.completed. Never trust guest/model text as evidence.
    """
    if not NONCE.fullmatch(nonce) or not 0 <= step <= 3 or len(raw) > 1048576:
        raise TransportFailure("invalid synthetic transport frame parameters")
    try:
        source = raw.decode("utf-8").replace("\r\n", "\n")
    except UnicodeDecodeError:
        raise TransportFailure("invalid synthetic transport encoding") from None
    terminal = None
    done = False
    for frame in source.split("\n\n"):
        if not frame.strip():
            continue
        fields = [line.removeprefix("data:").strip() for line in frame.splitlines()
                  if line.startswith("data:")]
        event = [line.removeprefix("event:").strip() for line in frame.splitlines()
                 if line.startswith("event:")]
        if len(fields) != 1 or len(event) > 1:
            raise TransportFailure("malformed synthetic Responses frame")
        if fields[0] == "[DONE]":
            if terminal is None or done or event:
                raise TransportFailure("out-of-order synthetic stream ending")
            done = True
            continue
        if terminal is not None or done:
            raise TransportFailure("extra data after synthetic completion")
        try:
            payload = json.loads(fields[0])
        except ValueError:
            raise TransportFailure("invalid synthetic Responses JSON") from None
        kind = payload.get("type") if isinstance(payload, dict) else None
        if not isinstance(kind, str) or (event and event[0] != kind):
            raise TransportFailure("inconsistent synthetic Responses event")
        if kind in ("error", "response.failed", "response.incomplete"):
            raise TransportFailure("synthetic Responses call failed")
        if kind == "response.completed":
            terminal = payload.get("response")
    if not isinstance(terminal, dict) or terminal.get("status") != "completed":
        raise TransportFailure("synthetic Responses missing completion")
    expected_id = f"resp_{nonce}_{step}"
    response_id = terminal.get("id")
    if response_id != expected_id:
        shown_id = response_id.replace(nonce, "[NONCE]")[:96] if isinstance(response_id, str) else "<missing>"
        raise TransportFailure("synthetic Responses identity changed: " + shown_id)
    output = terminal.get("output")
    if not isinstance(output, list) or len(output) != 1 or not isinstance(output[0], dict):
        raise TransportFailure("synthetic Responses output count changed")
    item = output[0]
    if step < 3:
        if (item.get("type") != "function_call"
                or item.get("name") != STEP_TOOLS[step]
                or item.get("call_id") != f"call_{nonce}_{step}"
                or item.get("status") != "completed"):
            raise TransportFailure("synthetic function call identity changed")
        try:
            arguments = json.loads(item["arguments"])
        except (KeyError, TypeError, ValueError):
            raise TransportFailure("invalid synthetic function arguments") from None
        if not isinstance(arguments, dict):
            raise TransportFailure("invalid synthetic tool arguments")
    else:
        if item.get("type") != "message" or item.get("status") != "completed":
            raise TransportFailure("missing synthetic terminal message")
        content = item.get("content")
        if not isinstance(content, list) or len(content) != 1 or not isinstance(content[0], dict):
            raise TransportFailure("invalid synthetic terminal content")
        if content[0].get("text") != "synthetic-tools-complete:" + nonce:
            raise TransportFailure("synthetic terminal marker not returned")
    return terminal["id"], item.get("call_id")


def probe_gateway_tool_transport(request, gateway: str, scoped: str,
                                 upstream: str, fixture_token: str,
                                 alias: str, nonce: str):
    """Four HTTP responses via a real gateway; ZERO executed tools.

    request has the existing synthetic_gateway_probe.request signature.
    A fake function_call_output is only a protocol acknowledgement; the
    full agent/workspace acceptance remains a separate mandatory test.
    """
    if not NONCE.fullmatch(nonce):
        raise TransportFailure("invalid tool transport run nonce")
    previous_id = None
    previous_call = None
    for step in range(4):
        body = {"model": alias, "stream": True}
        if step == 0:
            body["input"] = "transport only nonce=" + nonce
            body["tools"] = TOOL_DEFINITIONS
        else:
            body["previous_response_id"] = previous_id
            body["input"] = [{
                "type": "function_call_output",
                "call_id": previous_call,
                "output": "transport-only synthetic acknowledgement, no tool executed",
            }]
        raw = request(gateway, "/v1/responses", scoped, body)
        previous_id, previous_call = completed_item(raw, nonce, step)
    try:
        observed = json.loads(request(upstream, "/_observed/" + nonce, fixture_token))
        count = observed.get("count")
    except (ValueError, TypeError, AttributeError):
        raise TransportFailure("upstream synthetic observation invalid") from None
    if type(count) is not int or count != 4:
        raise TransportFailure("upstream did not observe four transport turns")
    return True
