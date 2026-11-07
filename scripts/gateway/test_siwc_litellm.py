"""Opt-in real pinned LiteLLM tests with synthetic OAuth and mock HTTP transport.

AGINCTUS_TEST_PINNED_LITELLM=1 python -m unittest discover -s scripts/gateway -p test_siwc_litellm.py -v
No real account, network endpoint override, or authentication bypass is used.
"""

import asyncio
import importlib
import io
import json
import os
import socket
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import siwc
from test_siwc import FakeProvider


@unittest.skipUnless(os.environ.get("AGINCTUS_TEST_PINNED_LITELLM") == "1", "opt-in pinned LiteLLM environment required")
class LiteLLMTest(unittest.TestCase):
    def test_authenticated_proxy_responses_tools_refresh_errors_and_isolation(self):
        async def run():
            import httpx
            import litellm
            from litellm.constants import LITELLM_PROXY_MASTER_KEY_ALIAS
            from litellm.llms.custom_httpx.http_handler import AsyncHTTPHandler
            from litellm.proxy import proxy_server

            with tempfile.TemporaryDirectory() as root:
                store = siwc.Store(Path(root) / "state")
                fake = FakeProvider()
                owner = siwc.Owner(store, fake.provider)
                siwc.login(owner, "personal", fake.browser, output=io.StringIO(), timeout=1)
                workload_key = Path(root) / "key"
                workload_key.write_text("sk-synthetic-workload")
                workload_key.chmod(0o600)
                siwc.configure(owner, SimpleNamespace(route="siwc-coding", account="personal", model="fixture-model",
                               workload_key_file=str(workload_key), consent_connected_app=True, consent_background=True))
                # The DB-free proxy fixture uses real master-key authentication.
                # LiteLLM replaces that identity with a fixed nonsecret alias.
                # Production `bind` accepts only the workload-key hash, never this alias.
                state = store.read()
                state["routes"]["siwc-coding"]["key_hashes"] = [LITELLM_PROXY_MASTER_KEY_ALIAS]
                store.write(state)
                with patch.dict(os.environ, {"AGINCTUS_SIWC_STATE_DIR": str(store.root),
                                             "LITELLM_LOCAL_MODEL_COST_MAP": "True"}):
                    hook_module = importlib.import_module("litellm_siwc")
                    if hook_module.proxy_handler_instance is not None:
                        hook_module.proxy_handler_instance.close()
                    hook = hook_module.SIWCHook(owner)
                    hook_module.proxy_handler_instance = hook
                    calls = []
                    mode = ["complete"]
                    agent_calls = [0]

                    def upstream(request):
                        body = json.loads(request.content)
                        self.assertEqual(str(request.url), siwc.RESOURCE + "/responses")
                        self.assertEqual(request.headers["authorization"], "Bearer " + store.read()["accounts"]["personal"]["access_token"])
                        self.assertNotIn("chatgpt-account-id", request.headers)
                        self.assertNotIn("x-attacker", request.headers)
                        self.assertFalse(body["store"])
                        self.assertTrue(body["stream"])
                        self.assertNotIn("metadata", body)
                        calls.append(body)
                        if mode[0] == "error":
                            return httpx.Response(429, json={"error": {"code": "subscription_sharing_usage_limit_exceeded",
                                                  "message": "synthetic-secret-value"}}, headers={"x-request-id": "req_synthetic"})
                        if mode[0] == "incomplete":
                            return httpx.Response(200, stream=httpx.ByteStream(b'event: response.created\ndata: {"type":"response.created"}\n\n'),
                                                  headers={"content-type": "text/event-stream"})
                        item = {"id": "fc_fixture", "type": "function_call", "call_id": "call_fixture",
                                "name": "read_file", "arguments": '{"path":"fixture.txt"}', "status": "completed"}
                        if mode[0] == "agent":
                            agent_calls[0] += 1
                            item = {"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed",
                                    "content": [{"type": "output_text", "text": "SIWC_SYNTHETIC_OK", "annotations": []}]}
                        complete = {"id": "resp_fixture", "object": "response", "created_at": 1800000000,
                                    "status": "completed", "model": "fixture-model", "output": [item],
                                    "parallel_tool_calls": False, "tools": [], "tool_choice": "auto",
                                    "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
                        events = [{"type": "response.created", "response": {**complete, "status": "in_progress", "output": []}},
                                  {"type": "response.output_item.added", "output_index": 0,
                                   "item": {**item, "status": "in_progress", **({"content": []} if mode[0] == "agent" else {"arguments": ""})}}]
                        if mode[0] == "agent":
                            events += [{"type": "response.content_part.added", "output_index": 0, "content_index": 0,
                                        "item_id": "msg_fixture", "part": {"type": "output_text", "text": "", "annotations": []}},
                                       {"type": "response.output_text.delta", "output_index": 0, "content_index": 0,
                                        "item_id": "msg_fixture", "delta": "SIWC_SYNTHETIC_OK"},
                                       {"type": "response.output_text.done", "output_index": 0, "content_index": 0,
                                        "item_id": "msg_fixture", "text": "SIWC_SYNTHETIC_OK"}]
                        else:
                            events += [{"type": "response.function_call_arguments.delta", "output_index": 0,
                                        "item_id": "fc_fixture", "delta": item["arguments"]},
                                       {"type": "response.function_call_arguments.done", "output_index": 0,
                                        "item_id": "fc_fixture", "arguments": item["arguments"]}]
                        events += [{"type": "response.output_item.done", "output_index": 0, "item": item},
                                   {"type": "response.completed", "response": complete}]
                        raw = "".join("event: " + event["type"] + "\ndata: " + json.dumps(event) + "\n\n" for event in events)
                        return httpx.Response(200, stream=httpx.ByteStream(raw.encode()), headers={"content-type": "text/event-stream"})

                    hook.transport = httpx.MockTransport(upstream)
                    complete_event = b'event: response.completed\ndata: {"type":"response.completed","response":{"status":"completed"}}\n\n'
                    for invalid in (b"data: [DONE]\n\n", complete_event + complete_event,
                                    b'event: response.completed\ndata: {"type":"response.failed"}\n\n',
                                    b'event: response.completed\ndata: {"type":"response.completed","response":"bad"}\n\n'):
                        guarded = hook_module.TerminalStream(httpx.ByteStream(invalid))
                        wire = b"".join([chunk async for chunk in guarded])
                        self.assertIn(b"response.failed", wire)
                        self.assertNotIn(b'"status":"completed"', wire)

                    class Cancellable(httpx.AsyncByteStream):
                        closed = False
                        async def __aiter__(self):
                            yield b'event: response.created\ndata: {"type":"response.created"}\n\n'
                            await asyncio.sleep(60)
                        async def aclose(self):
                            self.closed = True
                    source = Cancellable()
                    iterator = hook_module.TerminalStream(source).__aiter__()
                    await iterator.__anext__()
                    await iterator.aclose()
                    self.assertTrue(source.closed)
                    config = Path(root) / "config.json"
                    config.write_text(json.dumps({
                        "model_list": [{"model_name": "siwc-coding", "model_info": {"mode": "responses"},
                                        "litellm_params": {"model": "openai/fixture-model", "api_base": siwc.RESOURCE,
                                                           "api_key": "aginctus-siwc:siwc-coding"}}],
                        "litellm_settings": {"callbacks": ["litellm_siwc.proxy_handler_instance"], "turn_off_message_logging": True},
                        "general_settings": {"master_key": "sk-synthetic-workload"},
                        "router_settings": {"num_retries": 0, "fallbacks": []}}))
                    try:
                        await proxy_server.initialize(config=str(config), telemetry=False)
                        # Explicit config initialization loads the real callback object.
                        self.assertIn(hook, litellm.callbacks)
                        client = httpx.AsyncClient(transport=httpx.ASGITransport(app=proxy_server.app), base_url="http://test")
                        async with client:
                            body = {"model": "siwc-coding", "input": [{"role": "user", "content": "synthetic"}],
                                    "store": False, "stream": True,
                                    "tools": [{"type": "function", "name": "read_file", "parameters": {"type": "object"}}]}
                            response = await client.post("/v1/responses", json=body)
                            self.assertIn(response.status_code, (401, 403))
                            self.assertEqual(calls, [])
                            headers = {"Authorization": "Bearer sk-synthetic-workload", "x-attacker": "never-forward"}
                            response = await client.post("/v1/responses", json=body, headers=headers)
                            self.assertEqual(response.status_code, 200, response.text)
                            self.assertIn('"response.completed"', response.text)
                            self.assertIn("call_fixture", response.text)
                            self.assertEqual(len(calls), 1)
                            self.assertEqual(calls[0]["tools"][0]["name"], "read_file")
                            for changes in ({"api_base": "https://evil.invalid"}, {"api_key": "bad"},
                                            {"temperature": 0.5}, {"store": True}, {"stream": False},
                                            {"previous_response_id": "resp_other"}, {"fallbacks": ["unrelated"]},
                                            {"num_retries": 10}, {"tools": [{"type": "file_search"}]}):
                                rejected = await client.post("/v1/responses", json={**body, **changes}, headers=headers)
                                self.assertIn(rejected.status_code, (400, 401, 403, 422), rejected.text)
                            self.assertEqual(len(calls), 1)
                            permission = hook_module.PERMISSION.set(("siwc-coding", LITELLM_PROXY_MASTER_KEY_ALIAS))
                            try:
                                with self.assertRaises(siwc.Error):
                                    await hook.async_pre_call_deployment_hook({"api_key": "ordinary-api-provider"}, "aresponses")
                            finally:
                                hook_module.PERMISSION.reset(permission)
                            fake.now += 3601
                            continuation = {**body, "input": [{"type": "function_call_output", "call_id": "call_fixture",
                                                               "output": "synthetic file contents"}]}
                            response = await client.post("/v1/responses", json=continuation, headers=headers)
                            self.assertEqual(response.status_code, 200, response.text)
                            self.assertEqual(fake.refreshes, 1)
                            self.assertEqual(calls[-1]["input"][0]["call_id"], "call_fixture")
                            mode[0] = "error"
                            response = await client.post("/v1/responses", json=body, headers=headers)
                            self.assertEqual(response.status_code, 429, response.text)
                            self.assertIn("subscription_sharing_usage_limit_exceeded", response.text)
                            details = response.json()["error"]["provider_specific_fields"]["siwc"]
                            self.assertEqual(details["error"]["code"], "subscription_sharing_usage_limit_exceeded")
                            self.assertEqual(details["request_id"], "req_synthetic")
                            self.assertEqual(response.headers["x-aginctus-siwc-error-code"], "subscription_sharing_usage_limit_exceeded")
                            self.assertEqual(response.headers["x-request-id"], "req_synthetic")
                            self.assertNotIn("synthetic-secret-value", response.text)
                            mode[0] = "incomplete"
                            response = await client.post("/v1/responses", json=body, headers=headers)
                            self.assertIn("response.failed", response.text)
                            self.assertNotIn('"response.completed"', response.text)
                            if os.environ.get("AGINCTUS_TEST_OPENCODE_BINARY"):
                                import uvicorn

                                mode[0] = "agent"
                                listener = socket.socket()
                                listener.bind(("127.0.0.1", 0))
                                listener.listen()
                                endpoint = f"http://127.0.0.1:{listener.getsockname()[1]}/v1"
                                server = uvicorn.Server(uvicorn.Config(proxy_server.app, lifespan="off", log_level="error"))
                                serving = asyncio.create_task(server.serve(sockets=[listener]))
                                observed_fields = []
                                validate = hook_module.validate_body
                                def observe(body, model):
                                    observed_fields.append(sorted(set(body) - hook_module.ALLOWED))
                                    return validate(body, model)
                                hook_module.validate_body = observe
                                while not server.started:
                                    await asyncio.sleep(0.02)
                                try:
                                    gateway = Path(root) / "agent-gateway"
                                    gateway.mkdir(mode=0o700)
                                    (gateway / "workload-key").write_text("sk-synthetic-workload")
                                    (gateway / "workload-key").chmod(0o600)
                                    (gateway / "gateway.json").write_text(json.dumps({"endpoint": endpoint,
                                                                                   "model": "siwc-coding", "protocol": "responses"}))
                                    environment = {"PATH": os.environ["PATH"], "HOME": root, "AGINCTUS_OPENCODE_DIR": str(gateway),
                                                   "OPENCODE_DISABLE_AUTOUPDATE": "1", "OPENCODE_DISABLE_DEFAULT_PLUGINS": "1",
                                                   "OPENCODE_DISABLE_MODELS_FETCH": "1",
                                                   **{"XDG_" + key + "_HOME": str(Path(root) / key.lower())
                                                      for key in ("DATA", "CONFIG", "CACHE", "STATE")}}
                                    launcher = Path(__file__).resolve().parents[2] / "guest/opencode/launch.py"
                                    process = await asyncio.create_subprocess_exec(
                                        sys.executable, str(launcher), os.environ["AGINCTUS_TEST_OPENCODE_BINARY"],
                                        "run", "--format", "json", "Reply with the synthetic marker only. Do not execute tools.",
                                        cwd=root, env=environment, stdin=asyncio.subprocess.DEVNULL,
                                        stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
                                    try:
                                        stdout, stderr = await asyncio.wait_for(process.communicate(), 30)
                                    except TimeoutError:
                                        process.kill()
                                        await process.wait()
                                        self.fail("Real OpenCode synthetic SIWC test timed out; field names: " + str(observed_fields))
                                    self.assertEqual(process.returncode, 0, "Real OpenCode synthetic SIWC request failed; field names: " + str(observed_fields))
                                    self.assertIn(b"SIWC_SYNTHETIC_OK", stdout)
                                    self.assertGreaterEqual(agent_calls[0], 1)
                                finally:
                                    hook_module.validate_body = validate
                                    server.should_exit = True
                                    await serving
                                    listener.close()
                            # Other API-key models remain unchanged by the SIWC hook.
                            other = {"model": "other", "input": "ordinary provider input", "stream": False}
                            self.assertIs(await hook.async_pre_call_hook(None, None, other, "aresponses"), other)
                            # No authorized request context means no cross-provider fallback into SIWC.
                            with self.assertRaises(siwc.Error):
                                await hook.async_pre_call_deployment_hook({"api_key": "aginctus-siwc:siwc-coding",
                                                    "api_base": siwc.RESOURCE, "model": "openai/fixture-model"}, "aresponses")
                            # The ordinary API-key provider still uses its own
                            # credential even with the SIWC extension installed.
                            checked = []
                            test = self
                            class OrdinaryTransport(AsyncHTTPHandler):
                                def __init__(self):
                                    pass
                                async def post(self, url, headers, **kwargs):
                                    test.assertEqual(headers["Authorization"], "Bearer sk-synthetic-ordinary")
                                    checked.append(True)
                                    return httpx.Response(200, stream=httpx.ByteStream(complete_event),
                                                          headers={"content-type": "text/event-stream"},
                                                          request=httpx.Request("POST", url))
                            normal = await litellm.aresponses(model="openai/fixture-model", input=body["input"],
                                                             api_key="sk-synthetic-ordinary", api_base=siwc.RESOURCE,
                                                             stream=True, client=OrdinaryTransport())
                            _ = [event async for event in normal]
                            self.assertEqual(checked, [True])
                            state = store.read()
                            siwc.clear_tokens(state["accounts"]["personal"])
                            store.write(state)
                            response = await client.post("/v1/responses", json=body, headers=headers)
                            self.assertEqual(response.status_code, 401)
                            self.assertIn("signed out", response.text)
                    finally:
                        for transport in hook.clients.values():
                            await transport.client.aclose()
                        hook.close()
                        litellm.callbacks = []
        asyncio.run(run())


if __name__ == "__main__":
    unittest.main()
