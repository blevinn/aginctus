"""LiteLLM custom callback entrypoint for the OpenCode Go session policy.

Requires pinned LiteLLM v1.103.0 and must be explicitly configured on its
runtime; merely shipping this file does not activate request forwarding.
"""
from importlib.metadata import version

if version("litellm") != "1.103.0":
    raise RuntimeError("Aginctus selective Go forwarding requires LiteLLM 1.103.0")

from litellm.integrations.custom_logger import CustomLogger
from opencode_go_forwarding import sanitize_go_headers


class AginctusGoSessionHook(CustomLogger):
    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        try:
            return sanitize_go_headers(data, call_type)
        except ValueError:
            # The pinned native Responses proxy translates HTTPException to an
            # OpenAI-compatible 400. Bare ValueError becomes an internal 500.
            # Keep policy diagnostics and attacker-controlled header values out
            # of the HTTP response; reject before any upstream call.
            from fastapi import HTTPException

            raise HTTPException(
                status_code=400,
                detail="Invalid OpenCode Go session metadata",
            ) from None


proxy_handler_instance = AginctusGoSessionHook()
