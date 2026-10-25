"""Opt-in scripted Responses function calls for a single disposable Go fixture.

This is a *model emulator*, not a tool executor or a source edit receipt.
Use only after the pinned OpenCode tool contract is measured. The real agent
must execute each call; independent host assertions prove modifications.
"""
from __future__ import annotations

import json
import re
import threading
import time

NONCE = re.compile(r"[a-zA-Z0-9_-]{8,64}\Z")
PREVIOUS = re.compile(r"resp_([a-zA-Z0-9_-]{8,64})_([0-3])\Z")
STEP_NAMES = ("read", "edit", "bash")
REQUIRED = {
    "read": {"filePath"},
    "edit": {"filePath", "oldString", "newString"},
    "bash": {"command"},
}
MAX_SESSIONS = 16


class SequenceError(ValueError):
    pass


def text_input(value):
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return json.dumps(value, separators=(",", ":"), ensure_ascii=True)
    raise SequenceError("unexpected Responses input")


def tool_definitions(body):
    tools = body.get("tools")
    if not isinstance(tools, list) or len(tools) > 128:
        raise SequenceError("missing or oversized tool definitions")
    definitions = {}
    for tool in tools:
        if not isinstance(tool, dict) or tool.get("type") != "function":
            continue
        name = tool.get("name")
        if not isinstance(name, str) or name not in REQUIRED:
            continue
        schema = tool.get("parameters")
        if name in definitions or not isinstance(schema, dict) or schema.get("type") != "object":
            raise SequenceError("invalid tool schema")
        properties = schema.get("properties")
        if not isinstance(properties, dict) or not REQUIRED[name].issubset(properties):
            raise SequenceError("pinned OpenCode tool schema does not match fixture")
        definitions[name] = schema
    if set(definitions) != set(REQUIRED):
        raise SequenceError("required OpenCode read/edit/bash tools were not declared")


def tool_arguments(step, workdir):
    if step == 0:
        return {"filePath": workdir + "/calc.go"}
    if step == 1:
        return {
            "filePath": workdir + "/calc.go",
            "oldString": "return a - b",
            "newString": "return a + b",
        }
    if step == 2:
        return {"command": "go test ./...", "workdir": workdir,
                "description": "Verify disposable Go fixture"}
    raise SequenceError("invalid synthetic tool step")


def fixture_response(nonce, step, workdir):
    output = []
    if step < 3:
        name = STEP_NAMES[step]
        output = [{
            "id": "fc_" + nonce + "_" + str(step),
            "type": "function_call",
            "status": "completed",
            "call_id": "call_" + nonce + "_" + str(step),
            "name": name,
            "arguments": json.dumps(tool_arguments(step, workdir), separators=(",", ":")),
        }]
    else:
        output = [{
            "id": "msg_" + nonce,
            "type": "message", "status": "completed", "role": "assistant",
            "content": [{"type": "output_text",
                         "text": "synthetic-tools-complete:" + nonce, "annotations": []}],
        }]
    return {
        "id": "resp_" + nonce + "_" + str(step),
        "object": "response", "created_at": int(time.time()),
        "model": "synthetic-test", "status": "completed", "output": output,
        "usage": {"input_tokens": 12, "output_tokens": 8, "total_tokens": 20},
    }


def stream(response):
    item = response["output"][0]
    name = "response.output_item.done"
    events = [
        ("response.created", {"type": "response.created",
                              "response": {**response, "status": "in_progress", "output": []}}),
        (name, {"type": name, "output_index": 0, "item": item}),
        ("response.completed", {"type": "response.completed", "response": response}),
    ]
    return ("".join(
        "event: " + typ + "\ndata: " + json.dumps(doc, separators=(",", ":")) + "\n\n"
        for typ, doc in events
    ) + "data: [DONE]\n\n").encode()


class ToolSequence:
    def __init__(self, workdir="/workspace"):
        if workdir != "/workspace":
            raise SequenceError("untrusted fixture workspace root")
        self.workdir = workdir
        self._lock = threading.Lock()
        self._steps = {}

    def next(self, body):
        """Emit one response only after an authenticated ordered continuation."""
        if not isinstance(body, dict) or body.get("model") not in (
            "synthetic-test", "openai/synthetic-test"
        ) or body.get("stream") is not True:
            raise SequenceError("invalid tool-mode request")
        previous = body.get("previous_response_id")
        with self._lock:
            if previous is None:
                prompt = text_input(body.get("input"))
                match = re.search(r"\bnonce=([a-zA-Z0-9_-]{8,64})\b", prompt)
                if match is None:
                    raise SequenceError("initial tool task is missing nonce")
                nonce = match.group(1)
                if not NONCE.fullmatch(nonce) or nonce in self._steps or len(self._steps) >= MAX_SESSIONS:
                    raise SequenceError("duplicate or over-capacity tool session")
                tool_definitions(body)
                step = 0
                self._steps[nonce] = 0
            else:
                match = PREVIOUS.fullmatch(previous) if isinstance(previous, str) else None
                if match is None:
                    raise SequenceError("invalid previous Responses identity")
                nonce, previous_step = match.group(1), int(match.group(2))
                if self._steps.get(nonce) != previous_step + 1 or previous_step >= 3:
                    raise SequenceError("duplicate or out-of-order tool continuation")
                inputs = body.get("input")
                if not isinstance(inputs, list):
                    raise SequenceError("missing actual tool continuation")
                outputs = [i for i in inputs if isinstance(i, dict) and i.get("type") == "function_call_output"]
                if len(outputs) != 1 or outputs[0].get("call_id") != "call_" + nonce + "_" + str(previous_step):
                    raise SequenceError("forged, duplicated or missing call result")
                if not isinstance(outputs[0].get("output"), str):
                    raise SequenceError("malformed function output")
                step = previous_step + 1
                self._steps[nonce] = step
            return nonce, fixture_response(nonce, step, self.workdir)
