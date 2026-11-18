"""Bounded, redacted tool-contract metadata from genuine OpenCode Responses requests.

Used only by disposable loopback tests. Never persist raw input, headers,
tool parameters/descriptions or prompt text; those can contain credentials.
A tool declaration does not prove that a tool was invoked or executed.
"""
import hashlib
import json
import re

MAX_TOOLS = 128
MAX_SCHEMA_BYTES = 65536
NAME = re.compile(r"[A-Za-z][A-Za-z0-9_-]{0,63}\Z")


class ContractError(ValueError):
    pass


def declarations(body: dict) -> tuple[tuple[str, str], ...]:
    """Return only sorted tool names and SHA-256 schema fingerprints."""
    raw = body.get("tools")
    if not isinstance(raw, list) or not 1 <= len(raw) <= MAX_TOOLS:
        raise ContractError("Responses request has no bounded tool declarations")
    output = []
    seen = set()
    for entry in raw:
        if not isinstance(entry, dict) or entry.get("type") != "function":
            raise ContractError("unsupported or malformed tool declaration")
        name = entry.get("name")
        schema = entry.get("parameters")
        if not isinstance(name, str) or not NAME.fullmatch(name) or name in seen:
            raise ContractError("invalid or duplicate tool name")
        if not isinstance(schema, dict) or schema.get("type") != "object":
            raise ContractError("tool arguments schema must be a JSON object")
        blob = json.dumps(schema, sort_keys=True, separators=(",", ":"), ensure_ascii=True)
        if len(blob.encode("utf-8")) > MAX_SCHEMA_BYTES:
            raise ContractError("oversized tool arguments schema")
        seen.add(name)
        output.append((name, hashlib.sha256(blob.encode("utf-8")).hexdigest()))
    return tuple(sorted(output))


def results(body: dict) -> tuple[str, ...]:
    """Observe function_call_output IDs, not their potentially sensitive results."""
    inputs = body.get("input")
    if not isinstance(inputs, list):
        return ()
    found = []
    for item in inputs:
        if not isinstance(item, dict):
            continue
        if item.get("type") == "function_call_output":
            call_id = item.get("call_id")
            if not isinstance(call_id, str) or not re.fullmatch(r"call_[a-zA-Z0-9_-]{1,80}", call_id):
                raise ContractError("invalid tool continuation call ID")
            if call_id in found:
                raise ContractError("duplicate tool continuation call ID")
            found.append(call_id)
    return tuple(found)
