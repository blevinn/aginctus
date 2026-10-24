"""Offline pinned LiteLLM forwarding probe; run inside stock v1.103.0 image.

No network requests, keys, model registration or callback modification.
Only predetermined header *names* and pass/block classifications are printed.
"""
import json
import sys
from pathlib import Path
from types import SimpleNamespace

import litellm
import yaml
from starlette.datastructures import Headers

from litellm.proxy import proxy_server
from litellm.proxy.litellm_pre_call_utils import (
    LiteLLMProxyRequestSetup,
    clean_headers,
)


def main(config_path):
    config = yaml.safe_load(Path(config_path).read_text(encoding="utf-8"))
    expected = {
        "model_list": [],
        "litellm_settings": {
            "model_group_settings": {
                "forward_client_headers_to_llm_api": ["aginctus-go-pilot"],
            },
        },
    }
    if config != expected:
        raise RuntimeError("refusing unexpected forwarding configuration")

    # These are fake sentinel strings, never production credentials.
    synthetic = {
        "Authorization": "Bearer synthetic-client",
        "x-api-key": "synthetic-provider",
        "x-litellm-api-key": "synthetic-litellm",
        "x-opencode-session-id": "ses_0000000000abcdefgh",
        "x-opencode-session": "ses_0000000000abcdefgh",
        "x-custom-secret": "synthetic-custom",
        "anthropic-beta": "synthetic-beta",
        "x-stainless-retry-count": "2",
        "User-Agent": "opencode/1.0.0",
    }
    cleaned = clean_headers(Headers(synthetic))
    auth = SimpleNamespace(team_model_aliases=None, team_id=None)
    old_groups = litellm.model_group_settings
    old_user_info = litellm.add_user_information_to_llm_headers
    old_router = proxy_server.llm_router
    try:
        litellm.model_group_settings = SimpleNamespace(
            forward_client_headers_to_llm_api=["aginctus-go-pilot"]
        )
        litellm.add_user_information_to_llm_headers = False
        proxy_server.llm_router = None
        def apply(model):
            data = {"model": model}
            return LiteLLMProxyRequestSetup.add_headers_to_llm_call_by_model_group(
                data=data, headers=cleaned, user_api_key_dict=auth
            ).get("headers", {})
        for model in ("aginctus-go-pilot", "unrelated-model"):
            forwarded = {k.lower(): v for k, v in apply(model).items()}
            print("model:", model)
            for name in sorted(synthetic):
                lower = name.lower()
                print(lower, "forwarded" if lower in forwarded else "not forwarded")
            if model == "aginctus-go-pilot":
                if "x-opencode-session-id" not in forwarded:
                    raise RuntimeError("native session header was not forwarded")
                if "authorization" in forwarded or "x-api-key" in forwarded:
                    raise RuntimeError("proxy client credential forwarding detected")
            elif forwarded:
                raise RuntimeError("model-scoped forwarding leaked to another alias")
    finally:
        litellm.model_group_settings = old_groups
        litellm.add_user_information_to_llm_headers = old_user_info
        proxy_server.llm_router = old_router
    print("PASS: pinned LiteLLM model-scoped helper classified synthetic headers")
    print("NOTE: this does not test native Responses HTTP transport or header value integrity")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: probe_model_group_forwarding.py <config-yaml>")
    main(sys.argv[1])
