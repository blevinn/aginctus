"""LiteLLM custom callback entrypoint for the OpenCode Go session policy.

Requires pinned LiteLLM v1.103.0 and must be explicitly configured on its
runtime; merely shipping this file does not activate request forwarding.
"""
from litellm.integrations.custom_logger import CustomLogger

from opencode_go_forwarding import sanitize_go_headers


class AginctusGoSessionHook(CustomLogger):
    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        return sanitize_go_headers(data, call_type)


proxy_handler_instance = AginctusGoSessionHook()
