"""SIWC transport inside LiteLLM, never a separate proxy or native chatgpt/ cache."""

import asyncio
import contextvars
import hashlib
import json
import os
import re
import time
from importlib.metadata import version

import httpx
from litellm.integrations.custom_logger import CustomLogger
from litellm.llms.custom_httpx.http_handler import AsyncHTTPHandler
from siwc import (
    RESOURCE,
    TERMINAL,
    AuthenticationRequired,
    Error,
    Owner,
    PlanPermissionRequired,
    ProviderError,
    Store,
)

if version("litellm") != "1.103.0":
    raise RuntimeError("Aginctus SIWC transport requires verified LiteLLM 1.103.0")

ALLOWED = {"model", "input", "instructions", "tools", "tool_choice", "parallel_tool_calls",
           "reasoning", "include", "text", "store", "stream", "prompt_cache_key"}
PERMISSION = contextvars.ContextVar("aginctus_siwc_permission", default=None)


def validate_body(body, model):
    if (not isinstance(body, dict) or body.get("model") != model or set(body) - ALLOWED
            or not isinstance(body.get("input"), list) or body.get("store") is not False
            or body.get("stream") is not True):
        raise Error("SIWC requires an approved model, array input, store:false, stream:true and supported fields")
    for item in body["input"]:
        if (not isinstance(item, dict) or item.get("role") == "system"
                or item.get("type") not in {None, "message", "function_call", "function_call_output",
                                            "custom_tool_call", "custom_tool_call_output", "reasoning"}):
            raise Error("Unsupported SIWC input item")
    choice = body.get("tool_choice", "auto")
    if not ((isinstance(choice, str) and choice in {"auto", "required", "none"})
            or (isinstance(choice, dict) and choice.get("type") in {"function", "custom"})):
        raise Error("Unsupported SIWC tool choice")
    tools = body.get("tools", [])
    if not isinstance(tools, list):
        raise Error("Unsupported SIWC tools")

    def check_tool(tool):
        if not isinstance(tool, dict):
            raise Error("Unsupported SIWC tool")
        if tool.get("type") == "namespace":
            if not isinstance(tool.get("tools"), list):
                raise Error("Invalid SIWC tool namespace")
            for child in tool["tools"]:
                if not isinstance(child, dict) or child.get("type") not in {"function", "custom"}:
                    raise Error("Unsupported SIWC namespaced tool")
        elif tool.get("type") not in {"function", "custom"}:
            raise Error("Only local function/custom tools are supported by the Aginctus SIWC profile")
    for tool in tools:
        check_tool(tool)


class TerminalStream(httpx.AsyncByteStream):
    def __init__(self, source):
        self.source = source

    async def __aiter__(self):
        buffer = b""
        terminal = False
        terminal_frame = None
        done_seen = False
        count = 0
        deadline = time.monotonic() + 300
        try:
            async for chunk in self.source:
                count += len(chunk)
                buffer += chunk
                if len(buffer) > 1 << 20 or count > 64 << 20 or time.monotonic() > deadline:
                    raise Error("SIWC stream limit exceeded")
                # OpenAI SSE framing permits LF and CRLF. Payload bytes stay intact.
                while b"\n\n" in buffer or b"\r\n\r\n" in buffer:
                    marker = b"\r\n\r\n" if b"\r\n\r\n" in buffer else b"\n\n"
                    frame, buffer = buffer.split(marker, 1)
                    data = b"\n".join(line[5:].lstrip(b" ") for line in frame.splitlines() if line.startswith(b"data:"))
                    if not data:
                        yield frame + marker
                        continue
                    if data == b"[DONE]" and terminal and not done_seen:
                        done_seen = True
                        continue
                    event = json.loads(data)
                    if not isinstance(event, dict) or terminal:
                        raise Error("SIWC stream has invalid terminal framing")
                    kind = event.get("type")
                    names = [line[6:].strip() for line in frame.splitlines() if line.startswith(b"event:")]
                    if len(names) > 1 or (names and names[0] != str(kind).encode()):
                        raise Error("SIWC stream event/type mismatch")
                    if kind == "response.completed":
                        if not isinstance(event.get("response"), dict) or event["response"].get("status") != "completed":
                            raise Error("SIWC completion status mismatch")
                        terminal = True
                        terminal_frame = frame + marker
                        continue
                    elif kind in {"response.failed", "response.incomplete", "error"}:
                        terminal = True
                    yield frame + marker
            if buffer.strip() or not terminal:
                raise Error("SIWC stream ended without terminal completion")
            if terminal_frame is not None:
                yield terminal_frame
            if done_seen:
                yield b"data: [DONE]\n\n"
        except (Error, ValueError, TypeError, RecursionError, httpx.HTTPError):
            yield (b'event: response.failed\ndata: {"type":"response.failed","response":'
                   b'{"status":"failed","error":{"code":"aginctus_siwc_stream_invalid",'
                   b'"message":"SIWC stream interrupted or invalid; no completed response"}}}\n\n')
        finally:
            await self.source.aclose()

    async def aclose(self):
        await self.source.aclose()


class AdmissionStream(httpx.AsyncByteStream):
    def __init__(self, source, lease):
        self.source, self.lease = source, lease

    async def __aiter__(self):
        try:
            async for chunk in self.source:
                yield chunk
        finally:
            await self.aclose()

    async def aclose(self):
        try:
            await self.source.aclose()
        finally:
            if self.lease is not None:
                self.lease.__exit__(None, None, None)
                self.lease = None


class SIWCTransport(AsyncHTTPHandler):
    def __init__(self, owner, route, transport=None):
        # Do not initialize LiteLLM's shared HTTP client: it can inherit proxy,
        # redirect, logging and credential state from unrelated providers.
        self.owner = owner
        self.route = route
        self.client = httpx.AsyncClient(timeout=httpx.Timeout(30, connect=10), follow_redirects=False,
                                        trust_env=False, transport=transport)

    async def post(self, url, *, json=None, data=None, **kwargs):
        lease = self.owner.store.lock("admin", timeout=0, shared=True)
        lease.__enter__()
        try:
            permission = PERMISSION.get()
            if ((self.owner.store.root / "pending.json").exists() or not permission
                    or self.owner.store.read()["routes"].get(permission[0]) != self.route):
                raise Error("SIWC configuration changed or maintenance is pending; retry after apply/login")
            response = await self._post(url, json=json, data=data, **kwargs)
            response.stream = AdmissionStream(response.stream, lease)
            return response
        except BaseException:
            lease.__exit__(None, None, None)
            raise

    async def _post(self, url, *, json=None, data=None, **kwargs):
        import json as json_module

        if url != RESOURCE + "/responses" or data is not None:
            raise Error("SIWC transport refuses any other method, endpoint or encoded body")
        validate_body(json, self.route["model"])
        try:
            token = await asyncio.to_thread(self.owner.access, self.route["account"])
        except Error as exc:
            status = 503
            if isinstance(exc, AuthenticationRequired) or (isinstance(exc, ProviderError) and exc.code in TERMINAL):
                status = 401
            elif isinstance(exc, PlanPermissionRequired):
                status = 403
            elif isinstance(exc, ProviderError):
                status = exc.status
            request = httpx.Request("POST", url)
            rejected = httpx.Response(status, json={"error": {"type": "siwc_error", "code": "aginctus_siwc_credentials",
                                                            "message": str(exc)}}, request=request,
                                     headers={"x-aginctus-siwc-error-code": "aginctus_siwc_credentials"})
            raise httpx.HTTPStatusError("SIWC credentials unavailable", request=request, response=rejected) from None
        request = self.client.build_request("POST", url, json=json, headers={
            "Authorization": "Bearer " + token, "Accept": "text/event-stream", "Content-Type": "application/json"})
        try:
            response = await self.client.send(request, stream=True)
        except httpx.HTTPError:
            raise Error("SIWC inference network failure; credentials preserved, no automatic retry") from None
        finally:
            # The wire has been sent. Keep credentials out of response.request,
            # exception traces and LiteLLM's hidden/logging parameters.
            request.headers.pop("Authorization", None)
        if response.status_code != 200:
            status = response.status_code
            request_id = response.headers.get("x-request-id", "")
            raw = b""
            try:
                async for chunk in response.aiter_bytes():
                    raw += chunk
                    if len(raw) > 65536:
                        break
                payload = json_module.loads(raw)
                obj = payload.get("error", {}) if isinstance(payload, dict) else {}
                code = obj.get("code")
                param = obj.get("param")
                if (not isinstance(code, str) or len(code) > 128
                        or not re.fullmatch(r"[A-Za-z0-9_]+", code)
                        or not code.startswith(("subscription_sharing_", "chatpass_v2_"))):
                    code = "siwc_upstream_error"
                if not isinstance(param, str) or len(param) > 80 or not all(c.isalnum() or c in "_.[]" for c in param):
                    param = None
            except (ValueError, httpx.HTTPError):
                code, param = "siwc_upstream_error", None
            finally:
                await response.aclose()
            safe = {"error": {"type": "siwc_error", "code": code, "param": param,
                              "message": "SIWC request rejected; check account permissions, model and ChatGPT usage settings"}}
            if re.fullmatch(r"[A-Za-z0-9_-]{1,128}", request_id):
                safe["request_id"] = request_id
            rejected = httpx.Response(status, json=safe, headers={"x-request-id": request_id,
                                      "x-aginctus-siwc-error-code": code,
                                      "x-aginctus-siwc-error-param": param or ""}, request=request)
            rejected.raise_for_status()
            raise Error("Unexpected SIWC provider status")
        response.stream = TerminalStream(response.stream)
        return response


class SIWCHook(CustomLogger):
    def __init__(self, owner=None, transport=None):
        super().__init__(turn_off_message_logging=True)
        self.owner = owner or Owner(Store(os.environ["AGINCTUS_SIWC_STATE_DIR"]))
        self.transport = transport
        # A worker owns this lease until it exits; CLI mutations cannot race it.
        # Multiple LiteLLM workers are intentionally rejected, not dual owners.
        self.lease = self.owner.store.lock("runtime", timeout=0)
        self.lease.__enter__()
        self.clients = {}
        try:
            with self.owner.store.lock("admin"):
                self.owner.store.accept_pending()
                self.routes = self.owner.store.read()["routes"]
        except Exception:
            self.close()
            raise

    def close(self):
        self.lease.__exit__(None, None, None)

    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        from fastapi import HTTPException

        PERMISSION.set(None)
        alias = data.get("model", "")
        if str(alias).startswith("siwc-"):
            try:
                with self.owner.store.lock("admin", timeout=0, shared=True):
                    self.routes = self.owner.store.read()["routes"]
            except Error:
                raise HTTPException(503, "SIWC enrollment or configuration maintenance is in progress") from None
        if alias not in self.routes:
            if str(alias).startswith("siwc-") or str(data.get("api_key", "")).startswith("aginctus-siwc:"):
                raise HTTPException(403, "Unbound SIWC route")
            return data
        route = self.routes[alias]
        key = getattr(user_api_key_dict, "api_key", None)
        if isinstance(key, str) and key.startswith("sk-"):
            key = hashlib.sha256(key.encode()).hexdigest()
        if (call_type != "aresponses" or key not in route["key_hashes"]
                or not route.get("consent_connected_app") or not route.get("consent_background")):
            raise HTTPException(403, "SIWC is limited to the consenting user's connected Aginctus workload")
        if any(k in data for k in ("api_key", "api_base", "base_url", "client", "extra_headers", "extra_body",
                                   "fallbacks", "context_window_fallback_dict", "model_list", "mock_response",
                                   "num_retries", "max_retries", "retry_policy", "max_fallbacks", "default_fallbacks",
                                   "context_window_fallbacks", "content_policy_fallbacks", "deployment_id",
                                   "specific_deployment", "custom_llm_provider", "caching", "cache")):
            raise HTTPException(400, "SIWC client credential, route and fallback overrides are forbidden")
        # OpenCode's generic Responses client uses system messages. SIWC's
        # documented equivalent is developer messages; preserve their content.
        if isinstance(data.get("input"), list):
            data["input"] = [{**item, "role": "developer"} if isinstance(item, dict) and item.get("role") == "system"
                             else item for item in data["input"]]
        try:
            body = {k: v for k, v in data.items() if k in ALLOWED}
            body["model"] = route["model"]
            validate_body(body, route["model"])
            # Reject unsupported public request fields before LiteLLM can drop them.
            forbidden = {"background", "conversation", "max_output_tokens", "max_tool_calls", "metadata", "moderation",
                         "multi_agent", "prompt", "prompt_cache_retention", "safety_identifier", "temperature",
                         "top_logprobs", "top_p", "truncation", "user", "previous_response_id", "service_tier"}
            if forbidden.intersection(data):
                raise Error("Unsupported SIWC request fields: " + ", ".join(sorted(forbidden.intersection(data))))
        except Error as exc:
            raise HTTPException(400, str(exc)) from None
        PERMISSION.set((alias, key))
        data["caching"] = False
        return data

    async def async_pre_call_deployment_hook(self, kwargs, call_type):
        marker = kwargs.get("api_key", "")
        if not isinstance(marker, str) or not marker.startswith("aginctus-siwc:"):
            if PERMISSION.get() is not None:
                raise Error("SIWC cannot fall back to another account, provider or billing path")
            return None
        alias = marker[len("aginctus-siwc:"):]
        route = self.routes.get(alias)
        permission = PERMISSION.get()
        if (not route or not permission or permission[0] != alias or permission[1] not in route["key_hashes"]
                or getattr(call_type, "value", call_type) != "aresponses"
                or kwargs.get("api_base") != RESOURCE
                or kwargs.get("model") not in {route["model"], "openai/" + route["model"]}):
            raise Error("SIWC resolved deployment does not match its explicit binding")
        record = self.owner.store.read()["accounts"].get(route["account"], {})
        if (record.get("client_id") != route["client_id"] or record.get("sub") != route["sub"]):
            raise Error("SIWC account binding changed; rebind after explicit consent")
        if alias in self.clients and self.clients[alias].route != route:
            await self.clients.pop(alias).client.aclose()
        if alias not in self.clients:
            self.clients[alias] = SIWCTransport(self.owner, route, self.transport)
        kwargs["client"] = self.clients[alias]
        kwargs["num_retries"] = 0
        kwargs["max_retries"] = 0
        kwargs["caching"] = False
        return kwargs

    async def async_post_call_failure_hook(self, request_data, original_exception, user_api_key_dict, traceback_str=None):
        if request_data.get("model") not in self.routes:
            return None
        response = getattr(original_exception, "response", None)
        headers = getattr(response, "headers", None) or getattr(original_exception, "headers", None) or {}
        code = headers.get("x-aginctus-siwc-error-code")
        param = headers.get("x-aginctus-siwc-error-param") or None
        request_id = headers.get("x-request-id", "")
        if code is None:
            # The pinned exception mapper embeds the sanitized HTTP JSON in its
            # message instead of retaining its body/headers. Decode only that
            # bounded object; never reflect arbitrary exception text.
            message = getattr(original_exception, "message", "")
            try:
                if not isinstance(message, str) or len(message) > 65536:
                    return None
                payload, _ = json.JSONDecoder().raw_decode(message[message.index("{"):])
                error = payload["error"]
                code, param = error["code"], error.get("param")
                request_id = payload.get("request_id", "")
            except (ValueError, KeyError, TypeError):
                return None
        if (not isinstance(code, str) or len(code) > 128
                or not code.startswith(("subscription_sharing_", "chatpass_v2_", "aginctus_siwc_", "siwc_upstream_"))):
            return None
        # The pinned proxy otherwise replaces provider machine codes with the
        # HTTP status. Preserve structured SIWC details and a machine-code
        # header through its supported provider_specific_fields extension.
        original_exception.code = code
        original_exception.param = param
        original_exception.provider_specific_fields = {"siwc": {"error": {"code": code, "param": param}}}
        original_exception.headers = {"cache-control": "no-store", "x-aginctus-siwc-error-code": code}
        if isinstance(request_id, str) and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", request_id):
            original_exception.headers["x-request-id"] = request_id
            original_exception.provider_specific_fields["siwc"]["request_id"] = request_id
        return None


# Import is explicit activation and fails closed without protected bindings.
proxy_handler_instance = SIWCHook()
