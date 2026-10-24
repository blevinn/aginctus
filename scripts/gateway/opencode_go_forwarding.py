"""Fail-closed OpenCode Go header policy for LiteLLM native Responses calls.

Consumes only LiteLLM's server-populated, credential-redacted request headers.
It never reads a bearer token and never fabricates a session identifier.
"""
import re

GO_ALIAS = "aginctus-go-pilot"
_SESSION = re.compile(r"ses_[A-Za-z0-9_-]{8,128}\Z", re.ASCII)
_USER_AGENT = re.compile(r"opencode/[A-Za-z0-9._+-]{1,60}\Z", re.IGNORECASE | re.ASCII)
_SESSION_HEADERS = ("x-opencode-session", "x-opencode-session-id")


def _request_headers(data):
    request = data.get("proxy_server_request")
    if not isinstance(request, dict) or not isinstance(request.get("headers"), dict):
        raise ValueError("OpenCode Go request headers unavailable")
    result = {}
    for name, value in request["headers"].items():
        if not isinstance(name, str) or not isinstance(value, str):
            raise ValueError("OpenCode Go invalid request header")
        lower = name.lower()
        if lower in result and result[lower] != value:
            raise ValueError("OpenCode Go conflicting request header")
        result[lower] = value
    return result


def sanitize_go_headers(data, call_type):
    """Rewrite only the one approved native Responses alias; preserve other routes.

    The caller must install this as an actual LiteLLM async_pre_call_hook.
    A missing/invalid native session fails closed instead of issuing a new one.
    """
    if call_type != "aresponses" or data.get("model") != GO_ALIAS:
        return data
    headers = _request_headers(data)
    sessions = [headers[name] for name in _SESSION_HEADERS if name in headers]
    if not sessions or any(not _SESSION.fullmatch(s) for s in sessions):
        raise ValueError("OpenCode Go requires a valid native session")
    if len(set(sessions)) != 1:
        raise ValueError("OpenCode Go session headers disagree")
    agent = headers.get("user-agent")
    if not isinstance(agent, str) or not _USER_AGENT.fullmatch(agent):
        raise ValueError("OpenCode Go requires an identifying client User-Agent")

    # Override LiteLLM's generic forwarded x-* headers, if configured. Never
    # forward Authorization, x-api-key, x-litellm-*, proxy metadata, or body-
    # supplied extra_headers to the provider. The provider API key remains in
    # the model's protected litellm_params, not this client-derived map.
    data.pop("extra_headers", None)
    data["headers"] = {"x-opencode-session": sessions[0], "User-Agent": agent}
    return data
