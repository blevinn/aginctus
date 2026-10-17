#!/usr/bin/env python3
"""Launch OpenCode with an Aginctus-only provider, never persisting credentials in Nix."""
import json
import os
from pathlib import Path
import stat
import sys
from urllib.parse import urlsplit

DEFAULT_DIR = "/var/lib/aginctus/opencode"


def load(directory):
    settings = Path(directory) / "gateway.json"
    key_path = Path(directory) / "workload-key"
    data = json.loads(settings.read_text(encoding="utf-8"))
    endpoint = data.get("endpoint")
    model = data.get("model")
    protocol = data.get("protocol", "responses")
    if not isinstance(endpoint, str) or not isinstance(model, str):
        raise ValueError("gateway endpoint and model must be strings")
    url = urlsplit(endpoint)
    if (
        url.scheme != "http"
        or not url.hostname
        or url.username is not None
        or url.password is not None
        or url.query
        or url.fragment
        or url.path.rstrip("/") != "/v1"
        or any(c.isspace() for c in endpoint)
    ):
        raise ValueError("gateway endpoint must be a private HTTP /v1 URL without credentials")
    if not model or model.strip() != model or "/" in model:
        raise ValueError("invalid gateway model alias")
    if protocol not in ("responses", "chat-completions"):
        raise ValueError("unsupported gateway protocol")
    info = key_path.stat()
    if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077:
        raise ValueError("workload-key must be a private regular file (mode 0600)")
    key = key_path.read_text(encoding="utf-8").strip()
    if not key or "\n" in key or "\r" in key:
        raise ValueError("missing or invalid workload key")
    return endpoint, model, protocol, key


def main():
    # Only a pinned binary is used in the production image. This script is
    # intentionally simple enough to test with a mock executable.
    if len(sys.argv) < 2:
        raise ValueError("OpenCode executable path is required")
    endpoint, model, protocol, key = load(os.environ.get("AGINCTUS_OPENCODE_DIR", DEFAULT_DIR))
    provider = {
        "npm": "@ai-sdk/openai" if protocol == "responses" else "@ai-sdk/openai-compatible",
        "name": "Aginctus LiteLLM",
        "options": {"baseURL": endpoint, "apiKey": "{env:AGINCTUS_LITELLM_KEY}"},
        "models": {model: {"name": model}},
    }
    environment = os.environ.copy()
    environment["AGINCTUS_LITELLM_KEY"] = key
    environment["OPENCODE_CONFIG_CONTENT"] = json.dumps({
        "$schema": "https://opencode.ai/config.json",
        "model": "aginctus/" + model,
        "provider": {"aginctus": provider},
    })
    os.execve(sys.argv[1], sys.argv[1:], environment)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        # Do not print paths, source JSON or credential-bearing exceptions.
        print("aginctus opencode: gateway configuration unavailable or invalid", file=sys.stderr)
        sys.exit(1)
